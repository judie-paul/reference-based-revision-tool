# Project: AI-Powered Question Paper Revision Platform

## 1. Project Overview

Build a web application that allows students to revise examination question papers using one or more uploaded documents as their primary source of information.

The application should allow a student to:

1. Upload an examination question paper.
2. Upload one or more reference documents containing study material.
3. Have the application extract and analyze the questions.
4. Generate answers using the uploaded reference documents.
5. Use external sources only when the uploaded documents do not contain sufficient information to answer a question.
6. Clearly indicate the source of every answer.

The central idea is that the uploaded reference documents are the **source of truth**.

This is not intended to be a general-purpose chatbot. It is a document-grounded examination revision tool.

Correctness, transparency, and reliable source attribution are more important than generating an answer to every question.

---

## 2. Core Requirements

### A. Document Upload

The application should allow users to upload:

- Examination question papers.
- Reference documents (one or multiple).

For the initial version, support:

- PDF.
- DOCX.

The user should be able to upload multiple reference documents for a single revision session.

Implement configurable upload limits:

- Maximum file size per document: 10 MB.
- Maximum number of reference documents per revision session: 5.
- Maximum question paper size: 10 MB.

These limits should be configurable rather than hardcoded throughout the application.

The application should reject files that exceed the configured limits and display a clear error message.

Validate file types and do not rely solely on file extensions.

### B. Document Text Extraction

Extract readable text from uploaded PDF and DOCX files.

Preserve useful structural information, particularly:

- Original filename.
- Document identity.
- Page numbers, where available.
- Headings and sections, where available.
- Paragraph boundaries.

Do not implement document chunking, passage retrieval, embeddings, vector databases, or full-text search for the initial version.

Instead, use a simpler approach: send the extracted reference document text directly to the LLM alongside the question.

### C. Context Size Management

Although uploaded files have a maximum file size, their extracted text may still exceed the LLM's context window.

Therefore:

1. Extract text from all uploaded reference documents.
2. Estimate the combined token count of the extracted text.
3. Check whether the extracted text, question, instructions, and expected answer fit within the configured LLM context budget.
4. If everything fits, send the complete extracted reference material to the LLM.
5. If it does not fit, inform the user that the uploaded documents exceed the supported context limit.

Do not silently truncate documents or omit parts of them.

Do not silently send only some documents when the user expects all uploaded documents to be considered.

Make the context budget configurable based on the selected LLM provider and model.

For the MVP, prioritize simplicity over sophisticated document processing.

### D. Question Paper Processing

The application should extract individual questions from the uploaded question paper.

It should:

- Identify question numbers.
- Recognize subquestions (e.g. 1(a), 1(b), 2(i)).
- Preserve the original ordering.
- Preserve the relationship between questions and their subquestions.
- Display questions in a readable format.

Do not assume that every examination paper follows exactly the same formatting.

If a question cannot be reliably extracted, make this apparent rather than silently omitting it.

### E. Answer Generation

This is the most important functionality.

For every question, follow this process:

**Step 1: Provide the reference material.**

Send the complete extracted text of all uploaded reference documents, together with the question, to the LLM.

**Step 2: Evaluate whether the documents contain sufficient information.**

There are three possible situations.

**Case 1: Complete answer found in uploaded documents**

- Generate the answer using the uploaded material.
- Do not consult external sources.
- Mark the answer as DOCUMENT-BASED.

**Case 2: Partial answer found in uploaded documents**

- Use the information available in the documents.
- Identify what information is missing.
- Search external sources only for the missing information.
- Clearly distinguish document-supported information from externally obtained information.
- Mark the answer as SUPPLEMENTED.

**Case 3: No sufficient answer found in uploaded documents**

- Search reliable external sources.
- Generate an answer using the external information.
- Clearly indicate that the answer was not found in the uploaded material.
- Mark the answer as EXTERNAL.

**Important rules:**

- Never pretend that information came from an uploaded document when it did not.
- Never invent citations, page numbers, document references, or URLs.
- Do not use external knowledge to override information contained in the reference documents.
- If uploaded documents contain contradictory information, point this out.
- If the information is ambiguous, explain the ambiguity.
- If no reliable answer can be found, say so rather than fabricating an answer.
- Answers should be educational and sufficiently detailed to help students understand the concepts.

The LLM must be explicitly instructed to base document-supported claims only on the uploaded reference material.

If the documents do not support a claim, the LLM must not present that claim as document-supported.

### F. External Search

External searching is a fallback mechanism, not the default answering method.

Only perform an external search when the LLM determines that the uploaded material does not contain sufficient information.

External search should be handled separately from document processing and answer generation.

For externally sourced information:

- Prefer reliable educational resources, textbooks, academic institutions, and reputable publications.
- Keep track of the actual URLs retrieved.
- Preserve the source title and relevant reference information.
- Do not invent external citations.
- Clearly distinguish externally obtained information from document-supported information.

Do not silently use the LLM's general knowledge as a substitute for external research.

If external searching is unavailable, inform the user rather than pretending that a web search was performed.

### G. Source Attribution and Answer Status

Source attribution must be implemented as part of the application's data model.

Do not depend on the LLM to reconstruct citations after generating an answer.

The application must maintain references to the actual uploaded documents and external sources used during answer generation.

Every answer must have one of three statuses:

**1. DOCUMENT-BASED**

The uploaded reference documents contained sufficient information to answer the question.

Display:
- Document filename.
- Page number, where available.
- Relevant section or heading, where available.

**2. SUPPLEMENTED**

The uploaded documents contained some relevant information, but external information was required to complete the answer.

Display:
- Which parts of the answer are supported by the uploaded documents.
- Relevant document filenames and page numbers.
- Which parts required external information.
- External source titles and URLs.

**3. EXTERNAL**

The uploaded documents did not contain sufficient information to answer the question.

Display:
- External source titles.
- External URLs.
- Relevant references.

These statuses must be visually distinguishable in the interface.

If an answer combines document-based and external information, the student should be able to identify which source supports each part.

Source attribution must be based on actual uploaded documents and external search results, not generated or guessed citations.

For document citations, require the LLM to identify the supporting page and relevant section where possible. Validate page references against the extracted document metadata. Never blindly trust generated citations.

---

## 3. Revision Interface

The main interface should display the uploaded question paper as an organized revision session.

The student should be able to:

- Navigate between questions.
- View a question and its generated answer.
- Expand or collapse answers.
- See the source status of each answer.
- Expand source information to see document references and external links.
- Move to the previous or next question.
- Regenerate an answer if necessary.

Keep the interface simple and focused on studying.

The three answer statuses should be visually distinguishable.

Avoid unnecessary features in the initial version.

---

## 4. Technology Requirements

These requirements are strict.

### Backend

Use Go for the entire backend.

Prefer the Go standard library wherever practical.

Use:
- `net/http` for HTTP handling.
- `html/template` for server-side HTML rendering.
- Standard Go project organization.

### Frontend

Do not use:
- React.
- Vue.
- Angular.
- Svelte.
- Any other frontend framework.

Use:
- Go HTML templates.
- CSS.
- Vanilla JavaScript only when necessary.

The application should primarily use server-rendered HTML.

Do not introduce a frontend build system or Node.js dependency.

### Database

If persistent storage is required, use SQLite.

Use `modernc.org/sqlite`, a pure Go SQLite implementation.

The database driver must support compilation on both Windows and Unix-like operating systems without requiring CGO.

Keep database interactions isolated from the rest of the application.

Do not introduce a database search engine or vector database for the MVP.

### AI Integration

The application will need an LLM for:

- Question analysis.
- Answer generation.
- Evaluating whether the uploaded documents sufficiently answer a question.

Design the AI integration so that the LLM provider can be changed without rewriting the application's core logic.

Use environment variables for API keys and configuration.

Do not hardcode API keys.

Keep AI integration separate from:

- Document processing.
- Question extraction.
- External search.
- Source attribution.

### External Search

External web searching should only happen when the reference documents do not provide sufficient information.

Keep this functionality separate from document processing.

The search provider should be replaceable without changing the core answering workflow.

---

## 5. Data Model and Persistence

Design the database around the application's main entities.

At a minimum, consider:

- Revision sessions.
- Uploaded documents.
- Extracted questions.
- Generated answers.
- Answer source references.

An answer should be associated with its question.

A source reference should identify whether it refers to an uploaded document or an external source.

For document references, preserve:
- Document ID.
- Original filename.
- Page number, where available.
- Section or heading, where available.

For external references, preserve:
- Source title.
- URL.
- Relevant metadata.

Avoid duplicating information unnecessarily.

The database schema should support retrieving an answer together with its associated sources.

Do not overcomplicate the schema for hypothetical future features.

---

## 6. Architecture and Code Quality

Keep the architecture simple, modular, and easy to understand.

Separate responsibilities into logical components, such as:

- HTTP handlers.
- HTML templates.
- Document upload and validation.
- Text extraction.
- Question extraction.
- Answer generation.
- External search.
- Source attribution.
- Database operations.

The answer-generation workflow should coordinate these components rather than implementing everything inside an HTTP handler.

Avoid:
- Unnecessary abstractions.
- Complicated interfaces.
- Premature optimization.
- Microservices.
- Overengineering.

The application should run as a single Go application.

Uploaded files must be handled safely. Do not expose internal filesystem paths to users.

---

## 7. Initial Scope (MVP)

Prioritize these features:

1. Upload a question paper.
2. Upload multiple reference documents.
3. Enforce configurable file-size and document-count limits.
4. Extract text from uploaded documents while preserving available page metadata.
5. Check that the extracted text fits within the LLM context budget.
6. Extract and display individual questions.
7. Send the complete reference material alongside each question.
8. Evaluate whether the uploaded material sufficiently answers each question.
9. Generate answers using uploaded material as the primary source.
10. Fall back to external sources only when necessary.
11. Track actual source references.
12. Display the three answer statuses.
13. Navigate between questions and their answers.

Do not implement:

- User accounts.
- Subscriptions.
- Payments.
- Social features.
- Complex administration functionality.
- Vector databases.
- Embedding generation.
- Document chunking and retrieval systems.

Keep the initial application usable without requiring a complicated deployment setup.

---

## 8. Development Instructions

I am using the free version of Claude, so keep implementation tasks reasonably small and avoid unnecessarily large changes.

Before writing substantial amounts of code:

1. Examine the requirements.
2. Propose a simple architecture.
3. Explain the important technology choices.
4. Identify technical challenges, particularly:
   - PDF and DOCX text extraction.
   - Preserving page numbers.
   - Question extraction.
   - LLM context limits.
   - Evaluating answer sufficiency.
   - Reliable source attribution.
5. Propose a sensible implementation order.
6. Identify the minimum dependencies required.

After presenting the architecture, begin implementing the MVP in small, manageable stages.

At each stage:

- Explain what you are implementing.
- Keep the code functional.
- Avoid unnecessary dependencies.
- Ensure the project compiles.
- Test important functionality before moving on.
- Do not make unrelated changes to previously working components.

Do not attempt to implement every feature in one massive step.

Do not assume that a particular external API or library is available without checking its documentation and compatibility.

If a requirement is ambiguous, ask me before making a major architectural decision.

---

## 9. Most Important Design Principles

**1. Documents first.**

The uploaded reference documents are the primary authority.

**2. Simplicity.**

For the MVP, use file-size limits and an LLM context-budget check instead of implementing a complicated retrieval system.

**3. No silent truncation.**

If all uploaded reference material cannot fit into the available context, inform the user. Never silently omit parts of the material.

**4. Reliable attribution.**

Source references must originate from actual uploaded documents or external search results. Never invent citations.

**5. Transparent fallback.**

External sources should only be used when the uploaded material is insufficient. Make this visible to the student.

**6. Correctness over completeness.**

It is better to tell a student that an answer could not be established from the available sources than to generate a convincing but unsupported answer.

**7. Maintainability.**

Build a functional MVP before introducing advanced features. Prefer simple, maintainable Go code.

The final application should be a reliable, document-grounded examination revision tool, not another general-purpose AI chatbot.