# Daily Speaking Practice (Next.js + Redux)

A demo speaking-practice app rewritten from static HTML into a modern React stack:

- Next.js (App Router)
- TypeScript
- Redux Toolkit + React Redux

## Run locally

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

## Ollama setup (daily questions)

The app now uses local Ollama to:
- generate 3 speaking questions for each day
- generate follow-up questions and useful words for a selected topic

1. Install and run Ollama locally.
2. Pull a model (default is `gemma3:12b`):

```bash
ollama pull gemma3:12b
```

3. (Optional) configure model/base URL via environment variables:

```bash
export OLLAMA_BASE_URL=http://127.0.0.1:11434
export OLLAMA_MODEL=gemma3:12b
```

## Available scripts

- `npm run dev` - start dev server
- `npm run lint` - run Next.js lint rules
- `npm run build` - production build
- `npm run start` - run production server
