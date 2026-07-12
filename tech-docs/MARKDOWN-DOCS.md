# Markdown Docs

This document is a placeholder for future design work around optional Markdown companion documents.

Markdown companion documents are not part of the MVP.

## Basic Idea

JSON and BSON documents may have a corresponding Markdown document stored with the same document ID.

The Markdown document is strictly optional.

For example, a document such as:

```text
01KWD65CFQPEZS7H1WJE4MK990.json
```

could have a companion Markdown document such as:

```text
01KWD65CFQPEZS7H1WJE4MK990.md
```

## Purpose

Markdown companion documents can be used for human-written or human-readable content related to the structured document.

Future versions may support certain forms of:

- keyword searching
- scoring
- tagging
- text-derived metadata
- other text-oriented indexing

The exact behavior is still to be determined.

## Relationship To Structured Documents

The structured JSON or BSON document remains the source-of-truth document.

The Markdown document is related content, not a replacement for the structured document.

If users want structured fields related to the Markdown content, they may add those fields to the corresponding JSON or BSON document.

For example, the structured document might include fields for tags, title, summary, language, or other metadata while the Markdown file contains the longer free-form content.
