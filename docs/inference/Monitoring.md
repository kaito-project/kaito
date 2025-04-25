# Prometheus Metrics

Inference service exposes Prometheus metrics for monitoring system stats.

## vLLM runtime

vLLM exposes Prometheus metrics at the `/metrics` endpoint. These metrics provide detailed insights into the system's performance, resource utilization, and request processing statistics. The following table lists all available metrics:

vLLM version: 0.8.2

> [!NOTE]
> vLLM V1 engine is now enabled by default. Check [here](https://docs.vllm.ai/en/latest/getting_started/v1_user_guide.html) for details.

| Category | Metric Name | Type | Description |
|----------|-------------|------|-------------|
| System Stats - Scheduler | `vllm:num_requests_running` | Gauge | Number of requests currently running on GPU |
| System Stats - Scheduler | `vllm:num_requests_waiting` | Gauge | Number of requests waiting to be processed |
| System Stats - Scheduler | `vllm:lora_requests_info`   | Gauge | Running stats on lora requests |
| System Stats - Scheduler | `vllm:num_requests_swapped` | Gauge | Number of requests swapped to CPU (DEPRECATED: KV cache offloading is not used in V1) |
| System Stats - Cache | `vllm:gpu_cache_usage_perc` | Gauge | GPU KV-cache usage. 1 means 100 percent usage |
| System Stats - Cache | `vllm:cpu_cache_usage_perc` | Gauge | CPU KV-cache usage. 1 means 100 percent usage (DEPRECATED: KV cache offloading is not used in V1) |
| System Stats - Cache | `vllm:cpu_prefix_cache_hit_rate` | Gauge | CPU prefix cache block hit rate (DEPRECATED: KV cache offloading is not used in V1) |
| System Stats - Cache | `vllm:gpu_prefix_cache_hit_rate` | Gauge | GPU prefix cache block hit rat (DEPRECATED: use `vllm:gpu_prefix_cache_queries` and `vllm:gpu_prefix_cache_hits in V1`) |
| System Stats - Cache | `vllm:gpu_cache_usage_perc` | Counter | GPU prefix cache queries, in terms of number of queried blocks (V1 only) |
| System Stats - Cache | `vllm:gpu_prefix_cache_hits` | Counter | GPU prefix cache hits, in terms of number of cached blocks (V1 only) |
| Iteration Stats | `vllm:num_preemptions_total` | Counter | Cumulative number of preemption from the engine |
| Iteration Stats | `vllm:prompt_tokens_total` | Counter | Number of prefill tokens processed |
| Iteration Stats | `vllm:generation_tokens_total` | Counter | Number of generation tokens processed |
| Iteration Stats | `vllm:iteration_tokens_total`  | Histogram | Number of tokens per engine_step |
| Iteration Stats | `vllm:time_to_first_token_seconds` | Histogram | Time to first token in seconds |
| Iteration Stats | `vllm:time_per_output_token_seconds` | Histogram | Time per output token in seconds |
| Request Stats | `vllm:e2e_request_latency_seconds` | Histogram | End to end request latency in seconds |
| Request Stats | `vllm:request_queue_time_seconds` | Histogram | Time spent in WAITING phase for request |
| Request Stats | `vllm:request_inference_time_seconds` | Histogram | Time spent in RUNNING phase for request |
| Request Stats | `vllm:request_prefill_time_seconds` | Histogram | Time spent in PREFILL phase for request |
| Request Stats | `vllm:request_decode_time_seconds` | Histogram | Time spent in DECODE phase for request |
| Request Stats | `vllm:request_prompt_tokens` | Histogram | Number of prefill tokens processed per request |
| Request Stats | `vllm:request_generation_tokens` | Histogram | Number of generation tokens processed per request |
| Request Stats | `vllm:request_max_num_generation_tokens` | Histogram | Maximum number of requested generation tokens |
| Request Stats | `vllm:request_params_n` | Histogram | The 'n' request parameter |
| Request Stats | `vllm:request_params_max_tokens` | Histogram | The 'max_tokens' request parameter |
| Request Stats | `vllm:request_success_total` | Counter | Count of successfully processed requests |
| Speculative Decoding | `vllm:spec_decode_draft_acceptance_rate` | Gauge | Speculative token acceptance rate (DEPRECATED: Unused in V1) |
| Speculative Decoding | `vllm:spec_decode_efficiency` | Gauge | Speculative decoding system efficiency (DEPRECATED: Unused in V1) |
| Speculative Decoding | `vllm:spec_decode_num_accepted_tokens_total` | Counter | Number of accepted tokens |
| Speculative Decoding | `vllm:spec_decode_num_draft_tokens_total` | Counter | Number of draft tokens |
| Speculative Decoding | `vllm:spec_decode_num_emitted_tokens_total` | Counter | Number of emitted tokens (DEPRECATED: Unused in V1) |
