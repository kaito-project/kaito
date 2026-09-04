# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Response generators and cross-chunk corpus for guardrails benchmarking."""

from dataclasses import dataclass

# Approximate 4 chars per token.
CHARS_PER_TOKEN = 4

# Safe filler text (no PII, no banned substrings).
_SAFE_WORD_POOL = [
    "The",
    "quick",
    "brown",
    "fox",
    "jumps",
    "over",
    "the",
    "lazy",
    "dog.",
    "Machine",
    "learning",
    "models",
    "process",
    "data",
    "efficiently.",
    "Cloud",
    "computing",
    "enables",
    "scalable",
    "infrastructure.",
    "Kubernetes",
    "orchestrates",
    "containerized",
    "applications.",
    "RAG",
    "retrieval",
    "augmented",
    "generation",
    "combines",
    "search",
    "with",
    "language",
    "models",
    "to",
    "produce",
    "grounded",
    "answers.",
]


def generate_safe_text(num_tokens: int) -> str:
    """Generate safe filler text of approximately `num_tokens` tokens."""
    target_chars = num_tokens * CHARS_PER_TOKEN
    parts = []
    total = 0
    idx = 0
    while total < target_chars:
        word = _SAFE_WORD_POOL[idx % len(_SAFE_WORD_POOL)]
        parts.append(word)
        total += len(word) + 1  # +1 for space
        idx += 1
    return " ".join(parts)[:target_chars]


def inject_at_offset(text: str, payload: str, char_offset: int) -> str:
    """Insert `payload` into `text` at the given character offset."""
    return text[:char_offset] + payload + text[char_offset:]


def generate_text_with_banned_word(
    num_tokens: int,
    banned_word: str,
    position: str = "middle",
) -> str:
    """Generate text containing a banned word at a specific position."""
    base = generate_safe_text(num_tokens)
    if position == "start":
        return f" {banned_word} " + base
    elif position == "end":
        return base + f" {banned_word} "
    else:  # middle
        mid = len(base) // 2
        return inject_at_offset(base, f" {banned_word} ", mid)


def generate_text_with_email(num_tokens: int) -> str:
    """Generate text containing an email address in the middle."""
    base = generate_safe_text(num_tokens)
    mid = len(base) // 2
    return inject_at_offset(base, " user@example.com ", mid)


def generate_text_with_phone(num_tokens: int) -> str:
    """Generate text with a phone number."""
    base = generate_safe_text(num_tokens)
    mid = len(base) // 2
    return inject_at_offset(base, " +1-555-867-5309 ", mid)


def generate_text_with_credit_card(num_tokens: int) -> str:
    """Generate text with a credit card number (Luhn-valid)."""
    base = generate_safe_text(num_tokens)
    mid = len(base) // 2
    # 4111111111111111 is a standard Luhn-valid test card number.
    return inject_at_offset(base, " 4111-1111-1111-1111 ", mid)


def generate_text_with_ip(num_tokens: int) -> str:
    """Generate text with an IP address."""
    base = generate_safe_text(num_tokens)
    mid = len(base) // 2
    return inject_at_offset(base, " 192.168.1.100 ", mid)


# ---------------------------------------------------------------------------
# Cross-chunk corpus
# ---------------------------------------------------------------------------


@dataclass
class CrossChunkTestCase:
    """A test case where banned content spans a chunk boundary."""

    name: str
    full_text: str
    chunk_size: int
    banned_word: str
    expected_action: str  # "block" or "redact"
    description: str


def build_cross_chunk_cases(
    banned_word: str = "SECRET_PROJECT",
) -> list[CrossChunkTestCase]:
    """Build cross-chunk test cases where banned_word straddles chunk boundaries.

    For each case, `chunk_size` determines how the mock server splits the response,
    and the banned word is positioned so it crosses a chunk boundary.
    """
    cases = []
    word_len = len(banned_word)

    # Case 1: Word split in the middle (e.g., "SECRET_" | "PROJECT")
    split_pos = word_len // 2
    prefix_pad = "A" * 50 + " "
    chunk_size = len(prefix_pad) + split_pos
    text = prefix_pad + banned_word + " " + "B" * 100
    cases.append(
        CrossChunkTestCase(
            name=f"split_middle_{split_pos}",
            full_text=text,
            chunk_size=chunk_size,
            banned_word=banned_word,
            expected_action="block",
            description=f"Word split at position {split_pos}: '{banned_word[:split_pos]}' | '{banned_word[split_pos:]}'",
        )
    )

    # Case 2: Only first char in chunk 1 (e.g., "S" | "ECRET_PROJECT")
    prefix_pad2 = "C" * 50 + " "
    chunk_size2 = len(prefix_pad2) + 1
    text2 = prefix_pad2 + banned_word + " " + "D" * 100
    cases.append(
        CrossChunkTestCase(
            name="split_first_char",
            full_text=text2,
            chunk_size=chunk_size2,
            banned_word=banned_word,
            expected_action="block",
            description=f"Only first char in chunk 1: '{banned_word[0]}' | '{banned_word[1:]}'",
        )
    )

    # Case 3: Only last char in chunk 2 (e.g., "SECRET_PROJEC" | "T")
    prefix_pad3 = "E" * 50 + " "
    chunk_size3 = len(prefix_pad3) + word_len - 1
    text3 = prefix_pad3 + banned_word + " " + "F" * 100
    cases.append(
        CrossChunkTestCase(
            name="split_last_char",
            full_text=text3,
            chunk_size=chunk_size3,
            banned_word=banned_word,
            expected_action="block",
            description=f"Only last char in chunk 2: '{banned_word[:-1]}' | '{banned_word[-1]}'",
        )
    )

    # Case 4: Word at holdback boundary (word ends exactly at holdback_len from end)
    # The holdback (default 256) should catch this.
    safe_prefix = "G" * 200 + " "
    text4 = safe_prefix + banned_word + " " + "H" * 300
    cases.append(
        CrossChunkTestCase(
            name="near_holdback_boundary",
            full_text=text4,
            chunk_size=10,  # Small chunks to stress the buffer
            banned_word=banned_word,
            expected_action="block",
            description="Word near holdback boundary with small chunk size",
        )
    )

    # Case 5: Non-match — word embedded in larger word (no word boundary)
    text5 = "G" * 50 + " MY_SECRET_PROJECT_ARCHIVE " + "H" * 100
    cases.append(
        CrossChunkTestCase(
            name="embedded_no_word_boundary",
            full_text=text5,
            chunk_size=10,
            banned_word=banned_word,
            expected_action="allow",  # Should NOT match because no word boundary
            description="Banned word embedded in larger word — should not match with word matching",
        )
    )

    return cases


def build_standard_request(
    prompt: str = "Tell me about the project.",
    model: str = "mock-model",
    stream: bool = False,
) -> dict:
    """Build a standard chat completion request dict."""
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": prompt},
        ],
        "stream": stream,
    }
