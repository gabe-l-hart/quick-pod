# Retrieval-augmented generation

Retrieval-augmented generation (RAG) combines a retriever with a generator:
a language model is conditioned on documents fetched at inference time
instead of relying solely on parametric memory.

Key ideas:
- The retriever retrieves relevant documents from a corpus (e.g. arXiv papers).
- The generator conditions on those documents at inference time.
- RAG reduces hallucination and keeps answers current.

Challenges: retrieval quality, index freshness, context window usage.
