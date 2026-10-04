# HV Launcher technical details

See the [README](README.md) for installation and everyday use. This page summarizes setup, managed game behavior, and development.

## Setup

- **LinUwUx Proton:** Install a selected archive into Steam after reviewing the destination. Existing folders are not overwritten. Restart Steam after installation.
- **LinUwUx runtime:** HV Launcher detects an existing `~/.local/bin/linuwux` and `~/.local/share/linuwux/LinUwUx.so` for the configured Decky user. It checks file permissions and architecture but does not install, update, execute, or verify the runtime. Follow the upstream installation instructions.
- **UMIP:** Supported Limine and GRUB setups can be changed after confirmation. Reboot to apply the change. Other bootloaders require manual setup.
- **CPUID module:** HV Launcher can install the module for the running kernel through DKMS and test whether it loads. Review and trust the selected source before continuing. Unsupported distributions require manual setup.

## Managed games

Enabling a shortcut saves its current Steam launch options and adds HV Launcher around them. The Steam target and working directory stay unchanged. Disabling restores the saved options while preserving edits made externally.

HV Launcher prepares CPU support before starting the saved command and stays open for the game session, so Steam can track the normal launch and stop lifecycle. If HV Launcher is missing, the original command runs without emulation. If setup fails safely, the game does not start and an error is reported.

Each managed game can use LinUwUx Proton or the LinUwUx runtime. Older settings without a method continue to use Proton. To change a method, disable the shortcut, select the method, and enable it again. Runtime launches keep HV Launcher outermost, followed by the LinUwUx wrapper and the original command. This preserves launch options such as Gamescope and leading environment assignments. There is no global method preference.

## CPU support and recovery

When the kernel supports CPUID faulting, no module changes are needed at game launch. Otherwise, HV Launcher temporarily unloads AMD KVM modules, loads CPUID emulation, and restores KVM after the last managed game closes. It never stops virtual machines. If it cannot determine who changed module state, it asks for recovery rather than making an unsafe change.

## Stored data and local setup

Game settings are stored in `$XDG_DATA_HOME/hv-launcher/config.json`, or `~/.local/share/hv-launcher/config.json` by default. Setup operations run locally. System changes such as bootloader, package, and DKMS operations require confirmation in the plugin.

## Development

Requirements: Task, Go 1.23+, Node.js, and npm.

```sh
npm install
task test
task package
```

`task package` builds the frontend and Linux amd64 backend, then creates `package/hv-launcher.zip` for Decky Loader.

For live Linux development, install [Air](https://github.com/air-verse/air) and run `task dev`. It watches the frontend and backend, then deploys the plugin to `$HOME/homebrew/plugins/hv-launcher`. The deploy task uses `sudo rsync`; configure a narrowly scoped `visudo` rule for the exact command shown by the task before running it.

## QAM visual fixtures

Build and deploy a fixture to inspect readiness states in Decky:

```sh
task frontend:visual FIXTURE=hypervisor-ready
task deploy
```

Available QAM fixtures: `native-ready`, `native-intel7`, `z1-extreme`, `z1-extreme-native`, `hypervisor-ready`, `setup-required`, `recovery-required`, and `unsupported`. Shortcut management continues to use backend data. A normal frontend build disables the fixture. Readiness workspace fixtures include Proton confirmation/install/success and UMIP/module review, progress, and result states.
