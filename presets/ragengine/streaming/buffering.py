from dataclasses import dataclass
from typing import Literal, Protocol


@dataclass(frozen=True)
class StreamScanDecision:
    action: Literal["pass"] = "pass"


class StreamScanner(Protocol):
    def inspect(self, text: str) -> StreamScanDecision: ...


class NoOpStreamScanner:
    def inspect(self, text: str) -> StreamScanDecision:
        return StreamScanDecision()


@dataclass(frozen=True)
class StreamBufferConfig:
    min_scan_chars: int = 0
    max_window_chars: int = 4096
    max_emit_chars: int = 4096
    holdback_chars: int = 0


class StreamBuffer:
    def __init__(
        self,
        config: StreamBufferConfig | None = None,
        scanner: StreamScanner | None = None,
    ) -> None:
        self._config = config or StreamBufferConfig()
        self._scanner = scanner or NoOpStreamScanner()
        self._pending_text = ""
        self._recent_text = ""

    @property
    def pending_text(self) -> str:
        return self._pending_text

    def push_text(self, text: str) -> list[str]:
        if not text:
            return []

        self._pending_text += text
        self._recent_text += text
        self._scan_pending_text_if_needed()
        return self._drain_emittable_text()

    def flush(self) -> list[str]:
        if not self._pending_text:
            return []

        self._scan_pending_text_if_needed()
        return self._drain_all_text()

    def _scan_pending_text_if_needed(self) -> None:
        scan_window = self._get_scan_window()
        if len(scan_window) < self._config.min_scan_chars:
            return

        decision = self._scanner.inspect(scan_window)
        if decision.action != "pass":
            raise NotImplementedError("Only pass decisions are supported in PR3")

    def _get_scan_window(self) -> str:
        max_window_chars = self._config.max_window_chars
        if max_window_chars <= 0:
            return self._recent_text

        return self._recent_text[-max_window_chars:]

    def _drain_emittable_text(self) -> list[str]:
        emittable_chars = len(self._pending_text) - max(self._config.holdback_chars, 0)
        if emittable_chars <= 0:
            return []

        return self._drain_text(emittable_chars)

    def _drain_all_text(self) -> list[str]:
        return self._drain_text(len(self._pending_text))

    def _drain_text(self, total_chars: int) -> list[str]:
        if total_chars <= 0:
            return []

        drained_chunks: list[str] = []
        max_emit_chars = self._config.max_emit_chars
        remaining_chars = total_chars

        while remaining_chars > 0:
            chunk_size = remaining_chars
            if max_emit_chars > 0:
                chunk_size = min(chunk_size, max_emit_chars)

            drained_chunks.append(self._pending_text[:chunk_size])
            self._pending_text = self._pending_text[chunk_size:]
            remaining_chars -= chunk_size

        return drained_chunks