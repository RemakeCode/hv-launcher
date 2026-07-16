"""Decky lifecycle shim for the HV Launcher Go service."""

import asyncio
import os
import signal
from pathlib import Path

import decky


class Plugin:
    def __init__(self):
        self.process = None

    async def _main(self):
        binary = Path(decky.DECKY_PLUGIN_DIR) / "bin" / "hv-launcher"
        if not binary.is_file():
            raise RuntimeError(f"Go backend is missing: {binary}")

        decky.logger.info("Starting Go backend: %s", binary)
        try:
            self.process = await asyncio.create_subprocess_exec(
                str(binary),
                start_new_session=True,
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
