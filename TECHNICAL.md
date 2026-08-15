# HV Launcher technical details

This document explains the guided setup, runtime behavior, recovery model, and development workflow. See the [README](README.md) for installation and everyday use.

## Guided readiness setup

### Proton

Choose the archive you obtained, review the Steam installation, and confirm the source before installing. HV Launcher checks the archive structure but cannot prove where it came from. Existing Proton folders are never overwritten. Restart Steam after a successful install so it can discover the new tool.

### LinUwUx runtime

The runtime is an alternative LinUwUx integration method, not a Proton replacement. Runtime setup uses the Decky user's home and an unprivileged worker to install `~/.local/bin/linuwux` and `~/.local/lib/liblinuwux.so`; it never writes to `compatibilitytools.d` and does not require root. Games still need a compatible GE-Proton or CachyOS Proton selected in Steam. Official Valve Proton is not currently supported by the upstream runtime.

Each user-triggered install, update, or repair resolves the latest stable `brcly/linuwux-runtime` GitHub release to one exact tag. The worker accepts no caller URL, downloads only `liblinuwux.so`, `linuwux.sh`, and `SHA256SUMS` from that tagged release, bounds each response, verifies the published payload checksums and any GitHub asset digests, and atomically activates the two runtime files. A failed operation keeps the prior valid installation. Update availability compares bounded `linuwux --version` output with the latest stable tag; checksum values validate downloads but do not identify the installed version. There are no automatic updates, offline asset picker, or runtime-removal controls in the initial implementation.

The plugin package does not contain LinUwUx runtime assets. They remain separately licensed under AGPL-3.0-or-later and are acquired from upstream at setup time. The runtime panel links to [LinUwUx by brcly](https://github.com/brcly/linuwux-runtime), and packaging must preserve this source and license distinction.

### UMIP

Supported Limine and GRUB configurations can be updated after confirmation. The change takes effect only after a reboot, and the readiness check remains blocked until the running system reports UMIP disabled. Unsupported bootloaders require manual setup. HV Launcher does not automatically undo a bootloader change.

### CPUID module

Choose the `cpuid_fault_emulation` ZIP you obtained and review the exact host dependency transaction, if one is available. The module is built for the running kernel through DKMS. If that module and version are already registered, HV Launcher stops without replacing them.

DKMS executes the reviewed `Makefile` as root, so continue only when you trust the source. The ZIP/source provenance warning is separate from the generated kernel module's signature metadata: a signer reported by `modinfo` only means that signature metadata is present, not that the running kernel trusts it. HV Launcher therefore runs a guarded **Test module** operation after installation and keeps that action available in Readiness. Only an actual key-rejection result recommends signing the generated module and enrolling or trusting its certificate through the distribution's MOK/key mechanism. Secure Boot and kernel lockdown are informational; they are not readiness gates.

The last module-load outcome is stored separately from `config.json` and the transition journal as `cpuid-module-outcome.json`. It contains only a version, `pending`/`verified`/`failed` state, and bounded diagnostic/remediation text—never a module hash or kernel-release identity. A saved result is diagnostic and never skips a later guarded load.

Supported mutable package families are CachyOS/Arch, Debian/Ubuntu/Mint, and Fedora/Nobara. Immutable or other distributions receive manual guidance instead of an automatic package transaction.

## How it works

### CPU support methods

When the kernel provides CPUID faulting directly, HV Launcher does not need to change any system modules when a game starts.

On systems that need emulation, HV Launcher performs these steps for a managed game:

1. Temporarily unload the AMD KVM modules.
2. Load and check the CPUID fault emulation module.
3. When the last managed game closes, unload emulation and restore the KVM modules.

### Game lifecycle

Enabling a shortcut saves its current Steam launch options before adding HV Launcher. The shortcut target and working directory are not changed.

When the game starts, a small launcher prepares the required CPU support and then runs the shortcut's original command. It stays active until the launcher or game closes so that Steam's normal `Play -> Launching -> Stop -> Play` behaviour is preserved. Heroic and Lutris shortcuts work as long as the process started by Steam remains open for the lifetime of the game.

If HV Launcher is unavailable, the shortcut still runs its original command without emulation. If HV Launcher is available but cannot safely prepare the system—for example, because KVM is in use—the game does not start and an error is reported.

Managed games store an optional `proton` or `runtime` method. Legacy version-one records without a method continue to use their existing Proton launch value and are not rewritten during configuration loading. Runtime mode keeps HV Launcher as the outer process and starts the absolute user-scoped `linuwux` wrapper as its child immediately before Steam's game command. Changing the default affects only future enablement. Changing an existing game's method is allowed only while Steam still contains the exact last value HV Launcher applied.

### Module ownership and recovery

HV Launcher never stops virtual machines. It records which module changes it made so it can safely restore them after a game or restart. If it cannot determine who made a change, it reports that recovery is required instead of changing the system. It also leaves CPUID emulation alone when it was already active before HV Launcher started.

### Local setup boundary

Setup requests stay on the device and use fixed operations. LinUwUx Proton archive work and runtime installation run with the Decky user's permissions through separate `linuwux-proton-worker` and `linuwux-runtime-worker` modes. Their request protocols remain independent because they validate different inputs and write to different user-owned destinations. Bootloader, package, and DKMS changes require an explicit one-use confirmation from the plugin and cannot be turned into arbitrary commands or module names by a local request. Structural archive checks improve safety but do not replace trusting the source you selected.

### Stored data

HV Launcher stores its game settings at `$XDG_DATA_HOME/hv-launcher/config.json`, or `~/.local/share/hv-launcher/config.json` when `XDG_DATA_HOME` is not set.

## Development

Build requirements are Task, Go 1.23+, Node.js, and npm. Air is only required for the live Linux development workflow described below.

```sh
npm install
task test
task package
```

`task package` builds the frontend and Linux amd64 Go binary, then creates `package/hv-launcher.zip`. The archive contains one `hv-launcher/` plugin directory with `plugin.json`, the ESM marker in `package.json`, `main.py`, `dist/`, and `bin/hv-launcher`, ready for Decky Loader installation.

For live Linux development, also install [Air](https://github.com/air-verse/air), then run `task dev`. Task deploys to `$HOME/homebrew/plugins/hv-launcher`. Air watches the Go backend while Task watches the frontend and plugin metadata. Successful builds recreate `package/hv-launcher` and deploy it with `sudo rsync`; the deploy task then touches the installed `main.py` so Decky observes the new revision and reloads the plugin.

```sh
task dev
```

The watcher uses non-interactive sudo. Add a narrowly scoped `visudo` rule for the exact `/usr/bin/rsync -a --delete <absolute-repository-path>/package/hv-launcher/ <decky-plugin-path>/` command before starting it.

### QAM visual fixtures

Build the frontend with spoofed readiness data to inspect QAM states on the real Decky UI without changing the backend:

```sh
task frontend:visual FIXTURE=hypervisor-ready
task deploy
```

Available fixtures are `native-ready`, `native-intel7`, `z1-extreme`, `z1-extreme-native`, `hypervisor-ready`, `setup-required`, `recovery-required`, and `unsupported`. The fixture affects only QAM status and configuration requests; shortcut management continues to use the backend. Run `task frontend` (or any normal build) and deploy again to disable the fixture.

Readiness workspace process fixtures are also available when building with `FIXTURE=proton-confirm`, `FIXTURE=proton-installing`, `FIXTURE=proton-success`, `FIXTURE=umip-choice`, `FIXTURE=umip-existing`, `FIXTURE=module-review`, `FIXTURE=module-installing`, `FIXTURE=module-success`, `FIXTURE=module-signing`, `FIXTURE=module-failure`, or `FIXTURE=module-manual`.
