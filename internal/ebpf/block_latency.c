//go:build ignore
// The above tag tells `go build` to skip this file entirely; it is compiled
// by bpf2go into BPF bytecode during `go generate`, not by the Go compiler.

// SPDX-License-Identifier: GPL-2.0
// BPF programs must be GPL-licensed to use GPL-only kernel helpers.

// block_latency.c traces Linux block I/O requests from submission
// (block_rq_issue) to completion (block_rq_complete) and accumulates
// the latencies into a power-of-2 histogram stored in a BPF array map.
//
// The histogram can be read from userspace via the Go loader in loader.go.
// Each bucket i represents requests whose latency fell in the range
// [2^(i-1) µs, 2^i µs). Bucket 0 catches anything < 1 µs.

#include "vmlinux.h"          // BTF-derived kernel type definitions (CO-RE)
#include <bpf/bpf_helpers.h>  // bpf_ktime_get_ns, bpf_map_*, bpf_printk
#include <bpf/bpf_tracing.h>  // SEC, tracepoint context helpers

// Number of histogram buckets. Bucket i covers [2^(i-1) µs, 2^i µs).
// 24 buckets covers up to ~8 seconds, enough to capture any realistic
// I/O latency including slow HDDs and saturated storage backends.
#define HIST_BUCKETS 24

// start_times: a hash map from (dev, sector) → submission timestamp in ns.
// Keyed by a u64 packing the device major:minor and start sector so we can
// match the issue event to the completion event for the same request.
// max_entries is a hard kernel limit; 10240 concurrent requests is generous
// for a single-node system.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, u64);
    __type(value, u64);
} start_times SEC(".maps");

// io_latency_hist: a fixed-size array histogram. Index i holds the count
// of I/O requests whose round-trip latency fell in [2^(i-1) µs, 2^i µs).
// Array maps are zero-initialised by the kernel, so no setup is needed
// before reading.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, HIST_BUCKETS);
    __type(key, u32);
    __type(value, u64);
} io_latency_hist SEC(".maps");

// pack_key combines the device identifier and the starting sector of the
// request into a single u64 key. Using dev+sector rather than the request
// struct pointer keeps us on the tracepoint ABI (which is stable across
// kernel versions) rather than requiring a kprobe into internal structs.
//
// Limitation: if two concurrent requests target the same (dev, sector) they
// will alias. In practice this is rare and the impact is a missed latency
// sample rather than incorrect data.
static __always_inline u64 pack_key(u32 dev, u64 sector)
{
    return ((u64)dev << 32) | (sector & 0xFFFFFFFF);
}

// bucket_for returns the histogram bucket index for a latency value in
// microseconds. Uses a right-shift loop rather than __builtin_clz so it
// compiles to a simple BPF bytecode sequence the verifier is happy with.
static __always_inline u32 bucket_for(u64 latency_us)
{
    u32 bucket = 0;
    u64 val = latency_us;

    // Unrolled manually because BPF doesn't allow backward branches in loops
    // without the loop pragma; keeping it explicit is cleaner for readability.
    if (val >= 1)  bucket = 1;
    if (val >= 2)  bucket = 2;
    if (val >= 4)  bucket = 3;
    if (val >= 8)  bucket = 4;
    if (val >= 16) bucket = 5;
    if (val >= 32) bucket = 6;
    if (val >= 64) bucket = 7;
    if (val >= 128)  bucket = 8;
    if (val >= 256)  bucket = 9;
    if (val >= 512)  bucket = 10;
    if (val >= 1024) bucket = 11;
    if (val >= 2048) bucket = 12;
    if (val >= 4096) bucket = 13;
    if (val >= 8192) bucket = 14;
    if (val >= 16384)  bucket = 15;
    if (val >= 32768)  bucket = 16;
    if (val >= 65536)  bucket = 17;
    if (val >= 131072) bucket = 18;
    if (val >= 262144) bucket = 19;
    if (val >= 524288) bucket = 20;
    if (val >= 1048576) bucket = 21;
    if (val >= 2097152) bucket = 22;
    if (val >= 4194304) bucket = 23;

    return bucket < HIST_BUCKETS ? bucket : HIST_BUCKETS - 1;
}

// trace_rq_issue fires when the block layer issues a request to the device
// driver. We record the current kernel monotonic timestamp so trace_rq_complete
// can compute the elapsed time.
SEC("tracepoint/block/block_rq_issue")
int trace_rq_issue(struct trace_event_raw_block_rq *ctx)
{
    u64 ts = bpf_ktime_get_ns();
    u64 key = pack_key(ctx->dev, ctx->sector);
    bpf_map_update_elem(&start_times, &key, &ts, BPF_ANY);
    return 0;
}

// trace_rq_complete fires when the device driver signals completion of a
// request. We look up the start time, compute the latency, and increment
// the appropriate histogram bucket.
SEC("tracepoint/block/block_rq_complete")
int trace_rq_complete(struct trace_event_raw_block_rq_completion *ctx)
{
    u64 key = pack_key(ctx->dev, ctx->sector);
    u64 *start = bpf_map_lookup_elem(&start_times, &key);
    if (!start)
        return 0; // no matching issue event (started before we attached)

    u64 delta_ns = bpf_ktime_get_ns() - *start;
    bpf_map_delete_elem(&start_times, &key);

    u64 delta_us = delta_ns / 1000;
    u32 bucket = bucket_for(delta_us);

    u64 *count = bpf_map_lookup_elem(&io_latency_hist, &bucket);
    if (count)
        __sync_fetch_and_add(count, 1); // atomic increment; safe across CPUs

    return 0;
}

char LICENSE[] SEC("license") = "GPL";
