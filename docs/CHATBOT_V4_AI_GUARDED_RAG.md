# Chatbot V4 - AI Guarded RAG

## Summary
This patch adds a guarded AI layer to the Alemancenter chatbot while preserving the existing deterministic support flows.

The chatbot now works in this order:

1. Intent rules and contextual flows.
2. Knowledge base answers.
3. Site search across articles, posts, and files.
4. Guarded AI fallback/improvement only when needed.

AI is not allowed to invent links or disclose user account existence. The frontend renders safe backend links/actions only.

## Environment variables

```env
CHATBOT_AI_ENABLED=true
CHATBOT_AI_PROVIDER=together
CHATBOT_AI_API_KEY=your-key # optional; falls back to TOGETHER_AI_API_KEY
CHATBOT_AI_BASE_URL=https://api.together.ai/v1
CHATBOT_AI_MODEL=openai/gpt-oss-20b
CHATBOT_AI_MODELS=openai/gpt-oss-20b,meta-llama/Llama-3.3-70B-Instruct-Turbo,Qwen/Qwen3-235B-A22B-Instruct-2507-tput,zai-org/GLM-5.1,google/gemma-3n-E4B-it
CHATBOT_AI_MAX_TOKENS=520
CHATBOT_AI_GUEST_LIMIT_10M=5
CHATBOT_AI_USER_LIMIT_10M=15
CHATBOT_AI_DAILY_LIMIT=1000
```

If `CHATBOT_AI_API_KEY` is empty, the code falls back to existing Together AI variables:

```env
TOGETHER_AI_API_KEY=
TOGETHER_AI_MODEL=
TOGETHER_AI_BASE_URL=
```

## Safety limits

- Guests: 5 AI calls per 10 minutes by default.
- Logged-in users: 15 AI calls per 10 minutes by default.
- Global daily country limit: 1000 AI calls by default.
- Redis is used for rate limiting.

## Safety rules

The AI layer is skipped for sensitive flows such as account lookup and privacy requests.

The prompt explicitly forbids:

- confirming whether an email/account exists;
- asking for passwords or verification codes;
- exposing API keys, JWTs, Redis keys, database information, stack traces, or internal paths;
- inventing links or files not returned by the backend.

## Database

AutoMigrate creates:

```txt
chat_ai_usage
```

This table records provider, model, status, reason, tokens, intent, and country for AI auditing and cost review.

## Frontend

The chat widget now supports:

- `ai_used`
- `ai_model`
- visual badge for AI-improved answers;
- clearer loading text.

