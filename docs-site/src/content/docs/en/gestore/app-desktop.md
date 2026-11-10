---
title: Desktop app
description: Porto on the office PC without internet - first run, local network, backups, secret key, remote mode, kiosk and peripherals.
sidebar:
  order: 20
---

The desktop app for **Windows, macOS and Linux** contains a full Porto server with its data (SQLite) on the office PC. It
works **without internet** and without any Coste360 service. Its settings are in *App desktop*.

## Installation and first run

Download the package for your system ([Choose how to install](/en/installazione/scegli-come-installare/#desktop-app))
and start it. On first run the welcome page opens: create the owner and the organization, then the marina. The same page
can instead restore a backup, or connect the app to an online Porto (remote mode).

Data live in the user's folder:

| System | Folder |
|---|---|
| macOS | `~/Library/Application Support/Porto` |
| Windows | `%LOCALAPPDATA%\Porto` |
| Linux | `~/.local/share/porto` |

## Secret key

The key that encrypts documents and files is kept in the system keychain. The owner can reveal it in *App desktop*
(re-entering the password): **keep it in a safe place**, it is needed to restore backups on another computer or to move
to an online server.

## Backups

The app takes a backup every day and one at every close, plus manual ones. It keeps 14 daily and 5 close backups; manual
ones are never deleted. You can choose the folder (an external disk too). Restore starts from the panel: a safety backup
is taken first, then the app restarts with the backup's data. Backups use the `porto restore` format and can be restored
on a server too.

## Local network

Off until the owner turns it on. With the local network, the other office PCs and the phones on the marina Wi-Fi use the
same data, over HTTPS with a locally generated certificate (port 8443). Phones pair with the QR in *Dispositivi* and
accept the certificate once; the app's certificate can also be installed on the phone so the panel starts without
network. Phones work while the app is open.

## Remote mode

The app can act as a workstation of an **online Porto** already running: data stay on the server, the app keeps the
device token and a **PIN** protects its settings. If the server is unreachable the panel still opens and the allowed
operations go into the offline queue.

## Kiosk mode

For the desk: the window takes the whole screen, with no menus or taskbar. Exit with **Cmd/Ctrl+Shift+K** and the PIN
(remote mode) or an owner's credentials.

## Peripherals

In *Periferiche* configure what is connected to the PC:

- **receipt printer** ESC/POS (80/58 mm) on the network or through the print queues of macOS and Linux;
- **label printer** ZPL;
- USB **QR reader** that types like a keyboard;
- local **fiscal register**: Porto ships the interface but no driver for real models.

## Updates

Once a day the app checks whether a newer version exists and shows a notice, sending only the version number and the
operating system. It never downloads or installs anything; the check can be turned off in the settings.

## Limits

Without a server reachable from the internet there is no public booking page, no online payments and no remote
signature. To get them, move to a server with a backup and a restore. The packages already build, but signing and
notarisation need Coste360's certificates: until then Windows and macOS show a warning on first run.
