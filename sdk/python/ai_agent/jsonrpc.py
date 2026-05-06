"""Internal JSON-RPC 2.0 client over stdio.

Speaks newline-delimited JSON to a subprocess (the Go ``agent --rpc`` binary
or any compatible peer). Supports both directions:

* **wrapper -> core**: ``call(method, params)`` returns the response result
  (or raises an :class:`AgentError`).
* **core -> wrapper**: handlers registered via ``set_request_handler`` are
  invoked when the core sends a request such as ``tool.execute``,
  ``guard.execute`` or ``verifier.execute``. Handlers are async coroutines
  returning the result dict.
* **core -> wrapper notifications**: handlers registered via
  ``set_notification_handler`` for ``stream.delta`` / ``stream.end`` /
  ``context.status``.

The class is fully async; it owns the subprocess lifecycle when used via
``connect_subprocess`` / ``close``. Tests can also drive it directly with
in-memory streams via the ``streams`` constructor.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
from typing import Any, Awaitable, Callable

from ai_agent.errors import AgentError, from_rpc_error

JSONRPC_VERSION = "2.0"

NotificationHandler = Callable[[dict[str, Any]], Awaitable[None] | None]
RequestHandler = Callable[[dict[str, Any]], Awaitable[dict[str, Any]]]


class JsonRpcClient:
    """Async JSON-RPC 2.0 client speaking newline-delimited JSON over streams.

    Either ``connect_subprocess`` (spawns a child process) or ``attach``
    (use existing readers/writers) is required before calling RPC methods.
    """

    def __init__(self) -> None:
        self._reader: asyncio.StreamReader | None = None
        self._writer: asyncio.StreamWriter | None = None
        self._proc: asyncio.subprocess.Process | None = None

        self._next_id = 0
        self._id_lock = asyncio.Lock()
        self._write_lock = asyncio.Lock()

        self._pending: dict[int, asyncio.Future[dict[str, Any]]] = {}
        self._notif_handlers: dict[str, NotificationHandler] = {}
        self._request_handlers: dict[str, RequestHandler] = {}

        self._reader_task: asyncio.Task[None] | None = None
        self._closed = asyncio.Event()
        self._stderr_task: asyncio.Task[None] | None = None
        self._stderr_buffer: list[str] = []

    # -- lifecycle -------------------------------------------------------

    async def connect_subprocess(
        self,
        binary_path: str,
        *,
        args: list[str] | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
    ) -> None:
        """Spawn ``binary_path`` and start the reader loop."""

        if self._reader_task is not None:
            raise RuntimeError("JsonRpcClient already connected")

        spawn_args = list(args) if args is not None else ["--rpc"]
        full_env = os.environ.copy()
        if env:
            full_env.update(env)

        try:
            self._proc = await asyncio.create_subprocess_exec(
                binary_path,
                *spawn_args,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=full_env,
                cwd=cwd,
            )
        except FileNotFoundError:
            raise FileNotFoundError(
                f"Agent binary not found: {binary_path!r}\n"
                "  Build it first:  go build -o agent ./cmd/agent/\n"
                "  Or pass the correct path via AgentConfig(binary='./path/to/agent')"
            ) from None
        except PermissionError:
            raise PermissionError(
                f"Agent binary is not executable: {binary_path!r}\n"
                "  Fix permissions:  chmod +x " + binary_path
            ) from None
        assert self._proc.stdin is not None and self._proc.stdout is not None
        self._reader = self._proc.stdout
        self._writer = self._proc.stdin
        self._reader_task = asyncio.create_task(
            self._read_loop(), name="ai_agent.jsonrpc.read_loop"
        )
        if self._proc.stderr is not None:
            self._stderr_task = asyncio.create_task(
                self._drain_stderr(self._proc.stderr),
                name="ai_agent.jsonrpc.stderr",
            )

    def attach(
        self,
        reader: asyncio.StreamReader,
        writer: asyncio.StreamWriter,
    ) -> None:
        """Attach to existing async streams (used in tests)."""

        if self._reader_task is not None:
            raise RuntimeError("JsonRpcClient already attached")
        self._reader = reader
        self._writer = writer
        self._reader_task = asyncio.create_task(
            self._read_loop(), name="ai_agent.jsonrpc.read_loop"
        )

    async def close(self) -> None:
        """Shut down the subprocess (if any) and the reader loop gracefully."""

        if self._writer is not None:
            try:
                if self._writer.can_write_eof():
                    self._writer.write_eof()
            except (OSError, RuntimeError):
                pass

        if self._proc is not None:
            try:
                await asyncio.wait_for(self._proc.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                self._proc.terminate()
                try:
                    await asyncio.wait_for(self._proc.wait(), timeout=2.0)
                except asyncio.TimeoutError:
                    self._proc.kill()
                    await self._proc.wait()

        if self._reader_task is not None:
            self._reader_task.cancel()
            with contextlib.suppress(asyncio.CancelledError, Exception):
                await self._reader_task
            self._reader_task = None
        if self._stderr_task is not None:
            self._stderr_task.cancel()
            with contextlib.suppress(asyncio.CancelledError, Exception):
                await self._stderr_task
            self._stderr_task = None

        # Cancel any remaining pending requests.
        for fut in list(self._pending.values()):
            if not fut.done():
                fut.set_exception(AgentError("connection closed"))
        self._pending.clear()

        self._closed.set()

    @property
    def stderr_output(self) -> str:
        """Captured stderr from the subprocess (for debugging)."""

        return "".join(self._stderr_buffer)

    # -- handler registration --------------------------------------------

    def set_notification_handler(
        self, method: str, handler: NotificationHandler
    ) -> None:
        self._notif_handlers[method] = handler

    def set_request_handler(self, method: str, handler: RequestHandler) -> None:
        self._request_handlers[method] = handler

    # -- RPC primitives --------------------------------------------------

    async def call(
        self,
        method: str,
        params: dict[str, Any] | None = None,
        *,
        timeout: float | None = None,
    ) -> Any:
        """Send a wrapper -> core request and await its result.

        Raises :class:`AgentError` (or a subclass) on JSON-RPC error responses,
        timeout, or transport failure. All error paths raise :class:`AgentError`
        so ``except AgentError`` is sufficient to catch all SDK errors.
        """

        if self._writer is None:
            raise AgentError("not connected")

        rpc_id = await self._next_request_id()
        loop = asyncio.get_running_loop()
        fut: asyncio.Future[dict[str, Any]] = loop.create_future()
        self._pending[rpc_id] = fut

        message: dict[str, Any] = {
            "jsonrpc": JSONRPC_VERSION,
            "method": method,
            "params": params or {},
            "id": rpc_id,
        }

        try:
            await self._write_message(message)
        except Exception as exc:
            self._pending.pop(rpc_id, None)
            raise AgentError(f"failed to send {method}: {exc}") from exc

        try:
            response = (
                await asyncio.wait_for(fut, timeout=timeout)
                if timeout is not None
                else await fut
            )
        except asyncio.TimeoutError:
            raise AgentError(
                f"RPC timeout after {timeout}s waiting for {method!r} (id={rpc_id})"
            ) from None
        finally:
            self._pending.pop(rpc_id, None)

        if "error" in response and response["error"] is not None:
            err = response["error"]
            raise from_rpc_error(
                code=int(err.get("code", -32603)),
                message=str(err.get("message", "unknown error")),
                data=err.get("data"),
            )
        return response.get("result")

    async def notify(self, method: str, params: dict[str, Any] | None = None) -> None:
        """Send a notification (no ``id``, no response expected)."""

        if self._writer is None:
            raise AgentError("not connected")
        message: dict[str, Any] = {
            "jsonrpc": JSONRPC_VERSION,
            "method": method,
            "params": params or {},
        }
        await self._write_message(message)

    # -- internal: writing -----------------------------------------------

    async def _write_message(self, message: dict[str, Any]) -> None:
        assert self._writer is not None
        data = (json.dumps(message, separators=(",", ":")) + "\n").encode()
        async with self._write_lock:
            self._writer.write(data)
            await self._writer.drain()

    async def _next_request_id(self) -> int:
        async with self._id_lock:
            self._next_id += 1
            return self._next_id

    # -- internal: reading -----------------------------------------------

    async def _read_loop(self) -> None:
        assert self._reader is not None
        try:
            while True:
                line = await self._reader.readline()
                if not line:
                    break
                try:
                    message = json.loads(line.decode())
                except json.JSONDecodeError:
                    # The core MUST NOT send invalid JSON; ignore but don't crash.
                    continue
                await self._dispatch(message)
        except asyncio.CancelledError:
            raise
        except Exception:
            # Any unexpected error means the transport is dead.
            pass
        finally:
            for fut in list(self._pending.values()):
                if not fut.done():
                    fut.set_exception(AgentError("connection closed"))
            self._pending.clear()
            self._closed.set()

    async def _drain_stderr(self, stream: asyncio.StreamReader) -> None:
        try:
            while True:
                chunk = await stream.read(4096)
                if not chunk:
                    return
                self._stderr_buffer.append(chunk.decode(errors="replace"))
        except asyncio.CancelledError:
            raise
        except Exception:
            return

    async def _dispatch(self, message: dict[str, Any]) -> None:
        # Response (no `method` field, has `id`)
        if "method" not in message and "id" in message:
            rpc_id = message.get("id")
            if rpc_id is None:
                return
            fut = self._pending.get(int(rpc_id))
            if fut is not None and not fut.done():
                fut.set_result(message)
            return

        # Request or notification
        method = message.get("method")
        params = message.get("params") or {}
        rpc_id = message.get("id")

        if method is None:
            return

        if rpc_id is None:
            # notification: stream.delta / stream.end / context.status
            handler = self._notif_handlers.get(method)
            if handler is None:
                return
            try:
                result = handler(params)
                if asyncio.iscoroutine(result):
                    await result
            except Exception:
                # Notifications never fail back to the peer.
                pass
            return

        # core -> wrapper request: tool.execute / guard.execute / verifier.execute
        handler = self._request_handlers.get(method)
        if handler is None:
            await self._send_error(
                rpc_id,
                code=-32601,
                message=f"method not found: {method}",
            )
            return

        try:
            result = await handler(params)
            await self._write_message(
                {"jsonrpc": JSONRPC_VERSION, "id": rpc_id, "result": result}
            )
        except AgentError as exc:
            await self._send_error(
                rpc_id,
                code=exc.code if exc.code is not None else -32603,
                message=str(exc),
            )
        except Exception as exc:
            await self._send_error(rpc_id, code=-32603, message=str(exc))

    async def _send_error(self, rpc_id: Any, *, code: int, message: str) -> None:
        await self._write_message(
            {
                "jsonrpc": JSONRPC_VERSION,
                "id": rpc_id,
                "error": {"code": code, "message": message},
            }
        )


__all__ = ["JsonRpcClient"]
