---
title: Monitoring
---

Inference service exposes Prometheus metrics for monitoring system stats.

## vLLM runtime

vLLM exposes Prometheus metrics at the `/metrics` endpoint. These metrics provide detailed insights into the system's performance, resource utilization, and request processing statistics. The following table lists the metrics that vLLM exposes with its default settings, and the metrics that LoRA adapters and speculative decoding add:

vLLM version: 0.32.0

| Category | Metric Name | Type | Description |
|----------|-------------|------|-------------|
| System Stats - Scheduler | `vllm:num_requests_running` | Gauge | Number of requests in model execution batches |
| System Stats - Scheduler | `vllm:num_requests_waiting` | Gauge | Number of requests waiting to be processed |
| System Stats - Scheduler | `vllm:num_requests_waiting_by_reason` | Gauge | Number of waiting requests by `reason`: `capacity` (waiting for scheduling capacity) or `deferred` (deferred by transient constraints such as the LoRA budget or a KV transfer). The sum over all reasons equals `vllm:num_requests_waiting` |
| System Stats - Scheduler | `vllm:engine_sleep_state` | Gauge | Engine sleep state, by `sleep_state`: `awake` is 1 when the engine is awake and 0 when it is sleeping, `weights_offloaded` is 1 at sleep level 1, `discard_all` is 1 at sleep level 2 |
| System Stats - Scheduler | `vllm:lora_requests_info` | Gauge | Running stats on LoRA requests. Exposed only when LoRA adapters are enabled |
| System Stats - Cache | `vllm:kv_cache_usage_perc` | Gauge | KV-cache usage. 1 means 100 percent usage |
| System Stats - Cache | `vllm:prefix_cache_queries_total` | Counter | Prefix cache queries, in terms of number of queried tokens |
| System Stats - Cache | `vllm:prefix_cache_hits_total` | Counter | Prefix cache hits, in terms of number of cached tokens |
| System Stats - Cache | `vllm:external_prefix_cache_queries_total` | Counter | External prefix cache queries from KV connector cross-instance cache sharing, in terms of number of queried tokens |
| System Stats - Cache | `vllm:external_prefix_cache_hits_total` | Counter | External prefix cache hits from KV connector cross-instance cache sharing, in terms of number of cached tokens |
| System Stats - Cache | `vllm:mm_cache_queries_total` | Counter | Multi-modal cache queries, in terms of number of queried items |
| System Stats - Cache | `vllm:mm_cache_hits_total` | Counter | Multi-modal cache hits, in terms of number of cached items |
| System Stats - Cache | `vllm:cache_config_info` | Gauge | Always 1. The labels carry the engine's cache configuration |
| Iteration Stats | `vllm:num_preemptions_total` | Counter | Cumulative number of preemption from the engine |
| Iteration Stats | `vllm:prompt_tokens_total` | Counter | Number of prefill tokens processed |
| Iteration Stats | `vllm:prompt_tokens_by_source_total` | Counter | Number of prompt tokens by `source` |
| Iteration Stats | `vllm:prompt_tokens_cached_total` | Counter | Number of cached prompt tokens (local and external) |
| Iteration Stats | `vllm:prompt_tokens_cached_by_source_total` | Counter | Prefix-cache hit tokens by the cache tier that supplied them (`source`): `device`, `host`, `disk`, `p2p` or `external_unspecified` |
| Iteration Stats | `vllm:generation_tokens_total` | Counter | Number of generation tokens processed |
| Iteration Stats | `vllm:iteration_tokens_total` | Histogram | Number of tokens per engine_step |
| Iteration Stats | `vllm:time_to_first_token_seconds` | Histogram | Time to first token in seconds |
| Iteration Stats | `vllm:inter_token_latency_seconds` | Histogram | Inter-token latency in seconds |
| Request Stats | `vllm:e2e_request_latency_seconds` | Histogram | End to end request latency in seconds |
| Request Stats | `vllm:request_queue_time_seconds` | Histogram | Time spent in WAITING phase for request |
| Request Stats | `vllm:request_inference_time_seconds` | Histogram | Time spent in RUNNING phase for request |
| Request Stats | `vllm:request_prefill_time_seconds` | Histogram | Time spent in PREFILL phase for request |
| Request Stats | `vllm:request_decode_time_seconds` | Histogram | Time spent in DECODE phase for request |
| Request Stats | `vllm:request_time_per_output_token_seconds` | Histogram | Time per output token in seconds, per request |
| Request Stats | `vllm:request_prompt_tokens` | Histogram | Number of prefill tokens processed per request |
| Request Stats | `vllm:request_generation_tokens` | Histogram | Number of generation tokens processed per request |
| Request Stats | `vllm:request_max_num_generation_tokens` | Histogram | Maximum number of requested generation tokens |
| Request Stats | `vllm:request_params_n` | Histogram | The 'n' request parameter |
| Request Stats | `vllm:request_params_max_tokens` | Histogram | The 'max_tokens' request parameter |
| Request Stats | `vllm:request_num_preemptions` | Histogram | Number of times a request was preempted |
| Request Stats | `vllm:request_prefill_kv_computed_tokens` | Histogram | New KV tokens computed during prefill, excluding cached tokens |
| Request Stats | `vllm:request_success_total` | Counter | Count of successfully processed requests, by `finished_reason` |
| Request Stats | `vllm:admission_rejections_total` | Counter | Number of admission attempts rejected by a queue limit, by `reason` |
| Model FLOPs Utilization | `vllm:estimated_flops_per_gpu_total` | Counter | Estimated number of floating point operations per GPU. Stays 0 unless vLLM runs with `--enable-mfu-metrics` |
| Model FLOPs Utilization | `vllm:estimated_read_bytes_per_gpu_total` | Counter | Estimated number of bytes read from memory per GPU. Stays 0 unless vLLM runs with `--enable-mfu-metrics` |
| Model FLOPs Utilization | `vllm:estimated_write_bytes_per_gpu_total` | Counter | Estimated number of bytes written to memory per GPU. Stays 0 unless vLLM runs with `--enable-mfu-metrics` |
| Speculative Decoding | `vllm:spec_decode_num_drafts_total` | Counter | Number of speculative decoding drafts. The metrics in this category are exposed only when [speculative decoding](./speculative-decoding.md) is enabled |
| Speculative Decoding | `vllm:spec_decode_num_draft_tokens_total` | Counter | Number of draft tokens |
| Speculative Decoding | `vllm:spec_decode_num_accepted_tokens_total` | Counter | Number of accepted tokens |
| Speculative Decoding | `vllm:spec_decode_num_accepted_tokens_per_pos_total` | Counter | Accepted tokens per draft `position` |

Features that are off by default, such as KV cache offloading, add metrics of their own. See [vLLM Production Metrics](https://docs.vllm.ai/en/stable/usage/metrics/) for the full list.

### Metrics renamed or removed since vLLM 0.8.2

Earlier versions of this page listed the metrics of vLLM 0.8.2. Dashboards, alerts and autoscaling rules that still use the old names return no data with the current vLLM version:

| vLLM 0.8.2 | vLLM 0.32.0 |
|------------|-------------|
| `vllm:gpu_cache_usage_perc` | `vllm:kv_cache_usage_perc` |
| `vllm:gpu_prefix_cache_queries_total` | `vllm:prefix_cache_queries_total` |
| `vllm:gpu_prefix_cache_hits_total` | `vllm:prefix_cache_hits_total` |
| `vllm:time_per_output_token_seconds` | `vllm:inter_token_latency_seconds` |
| `vllm:num_requests_swapped`, `vllm:cpu_cache_usage_perc`, `vllm:cpu_prefix_cache_hit_rate`, `vllm:gpu_prefix_cache_hit_rate` | removed |
| `vllm:spec_decode_draft_acceptance_rate`, `vllm:spec_decode_efficiency`, `vllm:spec_decode_num_emitted_tokens_total` | removed |
