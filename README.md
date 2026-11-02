# Revision Tool: document-grounded exam revision

A web application for revising past examination papers. You upload a question paper and up to five reference documents, which can be notes, textbook chapters or handouts. The app extracts the questions and answers each one **from your documents first**. It uses external web sources only when your documents are not enough, and every answer is labelled with where its information came from:

| Status | Meaning |
|---|---|
| **DOCUMENT-BASED** | Fully answered from your documents. Shows the filename, page and section for each part. |
| **SUPPLEMENTED** | Partly answered from your documents. Missing information was completed from web sources, and each part of the answer is labelled with its own source. |
| **EXTERNAL** | Your documents did not contain the answer. It is based on web sources only, with titles and URLs. |
| **NOT ANSWERED** | No reliable answer could be established. The app says so rather than guessing. |

The app does not trust the model's citations blindly:
- Every quote the model cites is checked against the extracted text of your documents. Page and section are taken from where the quote actually appears, and parts that cannot be verified are marked **Unverified**.
- External sources can only be URLs that the search API really returned.
- If your documents are too large for the model's context window, the app refuses them. Nothing is silently truncated.

## Project layout

```
build/   compiled executables (revisiontool.exe, revisiontool-linux-amd64)
code/    Go source code (module "revisiontool")
  cmd/revisiontool/   main program
  internal/config     settings from environment variables / .env
  internal/upload     size limits and file-type detection (magic bytes)
  internal/extract    PDF and DOCX text extraction with pages and headings
  internal/questions  question extraction and verification
  internal/llm        LLM providers (Anthropic, OpenAI-compatible)
  internal/search     external search provider (Tavily)
  internal/answer     document-first answering workflow and citation checks
  internal/store      SQLite persistence (pure Go, no CGO)
  internal/web        HTTP handlers, HTML templates, CSS, JS (embedded in the binary)
build.ps1, build.sh  build scripts
.env.example         configuration template
```

## Quick start with the prebuilt executable

1. Get an LLM API key (see [Getting an API key](#getting-an-api-key)).
2. Copy `.env.example` to `.env` in the folder you will run the program from, and paste your key into it.
3. Run the executable:
   - **Windows:** `build\revisiontool.exe`
   - **Linux:** `chmod +x build/revisiontool-linux-amd64 && ./build/revisiontool-linux-amd64`
4. Open <http://127.0.0.1:8080> in your browser.

The program creates `revision.db` (the SQLite database) in the current folder. Uploaded files are not stored; only their extracted text is kept.

## Building from source

You only need **Go 1.26 or newer** (<https://go.dev/dl/>). No C compiler, Node.js or other tools are required, because the SQLite driver is pure Go and the HTML, CSS and JS are embedded in the binary.

### Windows

```powershell
git clone <this repository>
cd reference-based-revision-tool
.\build.ps1          # runs the tests, then creates build\revisiontool.exe
.\build.ps1 -All     # also creates build\revisiontool-linux-amd64
```

If PowerShell blocks the script, run `powershell -ExecutionPolicy Bypass -File .\build.ps1` once. You can also build by hand:

```powershell
cd code
go build -o ..\build\revisiontool.exe .\cmd\revisiontool
```

### Linux (and macOS)

```sh
git clone <this repository>
cd reference-based-revision-tool
./build.sh           # runs the tests, then creates build/revisiontool
./build.sh all       # also cross-compiles build/revisiontool.exe and build/revisiontool-linux-amd64
```

Or build by hand:

```sh
cd code
CGO_ENABLED=0 go build -o ../build/revisiontool ./cmd/revisiontool
```

### Development commands (run inside `code/`)

```sh
go run ./cmd/revisiontool                 # run without building a binary
go test ./...                             # all tests (no API key needed; the LLM is faked)
go test ./internal/answer -run TestSupplemented -v   # a single test
go vet ./...
```

## Getting an API key

The app needs an API key for a language model. It can optionally use a second key for web search. Keys are read from environment variables or from a `.env` file, and are never stored in the code.

> **Note:** a Claude.ai chat subscription (Free, Pro or Max) does **not** include API access. API usage is billed separately.

### Option A: Anthropic Claude (default, recommended)

1. Go to <https://console.anthropic.com> and create an account.
2. Add a payment method or buy prepaid credits under **Settings → Billing**. The API is pay-as-you-go.
3. Open **Settings → API Keys**, click **Create Key**, and copy the key (it starts with `sk-ant-`). It is only shown once.
4. Put it in `.env`:
   ```
   LLM_PROVIDER=anthropic
   ANTHROPIC_API_KEY=sk-ant-...
   ```

The default model is `claude-opus-5-5`. To reduce cost, set `LLM_MODEL=claude-sonnet-5-5` or `LLM_MODEL=claude-haiku-5-5`. Your reference documents are sent with every question, so the app uses Anthropic prompt caching to make repeat questions in the same session much cheaper.

### Option B: any OpenAI-compatible API (including free tiers)

Set `LLM_PROVIDER=openai`, plus `OPENAI_API_KEY`, `OPENAI_BASE_URL` and `LLM_MODEL`. Also set `LLM_CONTEXT_WINDOW` to your model's context window so the size check is accurate.

- **Google Gemini (has a free tier):** sign in at <https://aistudio.google.com>, click **Get API key → Create API key**. Then set:
  ```
  LLM_PROVIDER=openai
  OPENAI_API_KEY=<your Gemini key>
  OPENAI_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai
  LLM_MODEL=<a current Gemini model id from AI Studio>
  LLM_CONTEXT_WINDOW=<that model's context window>
  ```
- **OpenAI:** create a key at <https://platform.openai.com/api-keys> (paid). Use `OPENAI_BASE_URL=https://api.openai.com/v1`.
- **Groq (free tier):** create a key at <https://console.groq.com/keys>. Use `OPENAI_BASE_URL=https://api.groq.com/openai/v1`. Free-tier rate limits are small, so large documents may hit them.
- **Ollama (local, free):** use `OPENAI_BASE_URL=http://localhost:11434/v1`. `OPENAI_API_KEY` can be any non-empty text.

The provider must support JSON-mode chat completions (`response_format: {"type": "json_object"}`). Free tiers change often, so check the provider's current terms and model list.

### Optional: external web search (Tavily)

Without a search key, the app never looks anything up online. Questions your documents cannot answer are clearly reported instead. To enable the fallback search:

1. Sign up at <https://app.tavily.com>. A free plan with a monthly allowance of searches was available at the time of writing.
2. Copy your API key (it starts with `tvly-`) into `.env` as `TAVILY_API_KEY=tvly-...`.

## Configuration reference

All settings are environment variables (or `.env` entries). See `.env.example`.

| Variable | Default | Purpose |
|---|---|---|
| `LLM_PROVIDER` | `anthropic` | `anthropic` or `openai` (any OpenAI-compatible API) |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | – | API key for the chosen provider |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Endpoint for the `openai` provider |
| `LLM_MODEL` | `claude-opus-5-5` (anthropic) | Model id (required for `openai`) |
| `LLM_EFFORT` | `medium` | Anthropic effort level: `low` to `max` |
| `LLM_CONTEXT_WINDOW` | 1,000,000 (anthropic) / 128,000 (openai) | Context budget used for the "documents fit" check |
| `LLM_MAX_OUTPUT_TOKENS` | 16,000 / 8,000 | Tokens reserved for each reply |
| `TAVILY_API_KEY` | – | Enables external search |
| `SEARCH_PROVIDER` | auto | `tavily` or `none` |
| `SEARCH_MAX_RESULTS` | 5 | Results per search query |
| `MAX_REFERENCE_FILE_MB` | 10 | Max size per reference document |
| `MAX_PAPER_FILE_MB` | 10 | Max size of the question paper |
| `MAX_REFERENCE_DOCS` | 5 | Max reference documents per session |
| `ADDR` | `127.0.0.1:8080` | Listen address. Use `0.0.0.0:8080` to allow other machines on your network to connect. |
| `DB_PATH` | `revision.db` | SQLite database file |

## How it works

1. **Upload.** Files are checked against the size and count limits. Their type is detected from the content (PDF header, DOCX ZIP structure), not from the extension.
2. **Extraction.** Text is extracted with page numbers, paragraphs and section headings:
   - PDFs use glyph positions.
   - DOCX files use Word's saved page-break markers. When a DOCX has no saved page breaks, its page numbers are shown as approximate (≈).
   - Scanned, image-only PDFs have no text layer and are rejected.
3. **Context check.** The token size of all reference text, plus instructions, the question and room for the answer, must fit the configured context window. Otherwise the upload is refused with an explanation.
4. **Questions.** The model splits the paper into numbered questions and sub-questions. Each question's text is matched back to the paper, and any paper text that no question accounts for is listed so missing questions are visible.
5. **Answers** (generated on demand, one question at a time):
   1. The model receives *all* reference text plus the question. It answers using only the documents, with a verbatim quote per part, and reports whether the documents cover the question completely, partially or not at all.
   2. Every quote is verified against the extracted pages.
   3. If the coverage is incomplete, the app searches the web for *only* the missing information. The model then writes the missing parts using those real results only.
   4. The status is computed from the verified sources.

## Limitations

- Scanned PDFs (images without a text layer) are not supported because there is no OCR.
- Tables, equations and diagrams are extracted as plain text at best.
- Token counts are estimated conservatively (about 3 characters per token), so the size check may refuse material that would just barely fit.
