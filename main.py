"""Decky lifecycle shim for the HV Launcher Go service."""

import asyncio
import base64
import hashlib
import hmac
import json
import os
import secrets
import signal
import time
from pathlib import Path

import decky

SETUP_SECRET_ENVIRONMENT = "HV_LAUNCHER_SETUP_SECRET"
SETUP_CAPABILITY_OPERATIONS = frozenset({"umip-apply", "module-install"})
SETUP_CAPABILITY_SECONDS = 60
MAX_SETUP_BINDING_BYTES = 4096


class Plugin:
    def __init__(self):
        self.process = None
        self._setup_secret = secrets.token_bytes(32)

    async def _main(self):
        binary = Path(decky.DECKY_PLUGIN_DIR) / "bin" / "hv-launcher"
        if not binary.is_file():
            raise RuntimeError(f"Go backend is missing: {binary}")

        decky.logger.info("Starting Go backend: %s", binary)
        try:
            environment = os.environ.copy()
            environment[SETUP_SECRET_ENVIRONMENT] = self._encode(
                self._setup_secret
            )
            self.process = await asyncio.create_subprocess_exec(
                str(binary),
                start_new_session=True,
                env=environment,
            )
        except OSError as error:
            decky.logger.error("Failed to start Go backend: %s", error)
            raise RuntimeError(f"Failed to start Go backend: {error}") from error

    async def _stop(self):
        process = self.process
        self.process = None
        if process is None or process.returncode is not None:
            return

        decky.logger.info("Stopping Go backend")
        try:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                await asyncio.wait_for(process.wait(), timeout=5)
            except asyncio.TimeoutError:
                decky.logger.warning("Go backend did not stop gracefully; sending SIGKILL")
                os.killpg(process.pid, signal.SIGKILL)
                await asyncio.wait_for(process.wait(), timeout=3)
        except ProcessLookupError:
            pass
        except Exception as error:
            decky.logger.error("Failed to stop Go backend: %s", error)

    async def _unload(self):
        await self._stop()

    async def _uninstall(self):
        await self._stop()

    async def issue_setup_capability(self, operation: str, binding: str):
        if (
            not isinstance(operation, str)
            or operation not in SETUP_CAPABILITY_OPERATIONS
        ):
            raise ValueError("Unsupported privileged setup operation")
        if not isinstance(binding, str) or not binding:
            raise ValueError("A non-empty setup binding is required")
        try:
            binding_size = len(binding.encode("utf-8"))
        except UnicodeEncodeError as error:
            raise ValueError("The setup binding is not valid UTF-8") from error
        if binding_size > MAX_SETUP_BINDING_BYTES or "\x00" in binding:
            raise ValueError("The setup binding is invalid or too large")

        now = int(time.time())
        claims = {
            "version": 1,
            "operation": operation,
            "binding": binding,
            "nonce": secrets.token_urlsafe(24),
            "issuedAt": now,
            "expiresAt": now + SETUP_CAPABILITY_SECONDS,
        }
        payload = json.dumps(
            claims, ensure_ascii=False, separators=(",", ":")
        ).encode("utf-8")
        encoded_payload = self._encode(payload)
        signature = hmac.new(
            self._setup_secret, encoded_payload.encode("ascii"), hashlib.sha256
        ).digest()
        return f"{encoded_payload}.{self._encode(signature)}"

    @staticmethod
    def _encode(value: bytes) -> str:
        return base64.urlsafe_b64encode(value).decode("ascii").rstrip("=")
