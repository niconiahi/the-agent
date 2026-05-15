# model

This package is a data definition. It describes an LLM target — not how to talk to it, just what it *is*. Endpoint, capabilities, limits, pricing.

It imports nothing internal. Like `message`, it's a root in the dependency graph.

## model.go — The Model struct

A `Model` has everything you need to know about an LLM before you talk to it:

- **ID** — the string you send in the API request body (e.g., `"kimi-k2.5"`). This is what the LLM provider uses to route your request to the right model.
- **Name** — human-readable, for display purposes. `"Kimi K2.5"`.
- **API** — the API protocol type. This is the key that the sender's provider registry uses to look up the right streaming implementation. Kimi uses the OpenAI-compatible API, so this is `"openai-completions"`. If you added Claude, it would be `"anthropic-messages"`. The sender doesn't hardcode providers — it dispatches based on this string.
- **Provider** — identifies which service this model belongs to. `"moonshot"` for Kimi. This is used for things like resolving which environment variable holds the API key.
- **BaseURL** — where to send HTTP requests. `"https://api.moonshot.cn/v1"` for Kimi. The sender appends `/chat/completions` to this.
- **Reasoning** — whether the model supports thinking/reasoning blocks. Kimi K2.5 does. This tells the sender whether to include thinking configuration in the request and whether to expect `reasoning_content` in the SSE chunks.
- **InputTypes** — what the model can accept. `["text", "image"]` for Kimi K2.5. This could be used to validate that you're not sending images to a text-only model.
- **CostPerMillion** — per-million-token pricing for input, output, cache reads, and cache writes. Lives on the model because pricing is a property of the model, not of the message.
- **ContextWindow** — total token capacity (262144 for Kimi K2.5). The orchestrator could use this to know when to stop and persist the conversation to avoid losing context.
- **MaxTokens** — maximum output tokens per response (32768). Passed through to the API request.

## KimiK25()

A constructor function that returns a `Model` hardcoded for Kimi K2.5. No configuration, no parameters — just returns the right values. This is fine for a POC. In a real system you might load model definitions from a config file or have a registry, but right now there's exactly one model and it doesn't change.

The `CostPerMillion` field is left at zero values because the Kimi pricing isn't filled in yet. The structure is there, the math works (via `message.CalculateCost`), but the actual dollar amounts aren't populated. When they are, cost tracking just starts working — no code changes needed.

## Why API and Provider are separate

A model has both an `API` type and a `Provider`. They're different concepts. The API type says *how* to talk to the model (what wire protocol, what JSON format). The Provider says *who* runs the model (where's the API key, what's the billing entity).

Two models from different providers can use the same API type. Kimi uses the OpenAI-compatible API. So does DeepSeek. So do a dozen other providers. One sender implementation (the `"openai-completions"` provider) handles all of them. But the API key comes from different environment variables because they're different providers.

This separation means adding a new model that speaks the OpenAI protocol is pure configuration — just a new constructor function like `KimiK25()`. You only need new sender code when you encounter a genuinely different wire protocol.
