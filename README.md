# Lefalock — User Guide

## Contents

- [About Lefalock](#about-lefalock)
- [Get started](#get-started)
- [What you can do](#what-you-can-do)
- [Installation](#installation)
- [Requirements](#requirements)
- [Everyday use](#everyday-use)
- [Console commands](#console-commands)
- [Handling secrets safely](#handling-secrets-safely)
- [Backups and moving to another computer](#backups-and-moving-to-another-computer)
- [When the vault will not open](#when-the-vault-will-not-open)
- [Storage, settings, and access](#storage-settings-and-access)
- [Updates](#updates)
- [Troubleshooting](#troubleshooting)
- [License and corresponding source](#license-and-corresponding-source)
- [Building the source](#building-the-source)

## About Lefalock

Lefalock keeps passwords, time-based one-time codes (TOTP), and notes in an
encrypted local vault. Use the full-screen terminal interface (TUI) or individual
console commands.

No application account is required, and there is no device synchronization.
The interface is in English; record names and notes support Unicode.
Access to the vault relies on your OS account and credential store; there is
no separate master password or automatic vault lock.

## Get started

1. Run the [installer for your OS](#installation). It downloads this release,
   verifies the archive's SHA256, installs Lefalock for your OS user, and
   configures PATH.
2. Follow [Start Lefalock](#start-lefalock) to activate PATH for your shell,
   then run `lefalock` from any directory.
   With no arguments, this starts the full-screen terminal interface (TUI).

   Your OS credential store must be available and unlocked. See
   [Requirements](#requirements) if the vault will not open.
3. The first launch creates a local vault and opens **Vault**. Choose
   **Add service**, enter service and secret names, then select **Password**
   or **Auth code**. Enter the value (passwords require confirmation) or
   choose **Generate password**, then save with **Create service**.
4. Open **Backups → Create backup**. Keep the `.lbcp` file and its 24-word
   recovery phrase separately. The phrase is required to restore that backup.

Continue with [Installation](#installation), [Everyday use](#everyday-use),
or [Console commands](#console-commands).

**Vault:** select a service on the left and its credentials on the right.

![Lefalock TUI: vault with example services and credentials](https://github.com/Lefalock/clients-cli/releases/download/v0.0.15/lefalock_v0.0.15_tui-vault.png)

**Add service:** enter the service name and its first credential, or generate
a password before saving.

![Lefalock TUI: create a service and its first secret](https://github.com/Lefalock/clients-cli/releases/download/v0.0.15/lefalock_v0.0.15_tui-create-service.png)

Screenshots show example data without revealed secrets. Images are attached
to the same GitHub release; viewing them requires network access.

## What you can do

- Store passwords, TOTP authenticator keys, and multiline notes locally.
- Generate passwords with a configurable length and character sets.
- Organize records into folders and services; rename, move, and search them.
- Copy passwords and one-time codes, or reveal a value when needed.
- Create encrypted backups and restore them on another computer.
- Rotate the vault encryption key.
- Use the terminal interface or console commands for individual operations.
- Check for application updates and install them from public GitHub Releases.

## Installation

Use the installer for your OS. No Go toolchain, administrator access, or
application account is required. Run it as your normal OS user, without sudo.
Close any running Lefalock client before installing or reinstalling.

| Your system | Installer | Architecture |
| --- | --- | --- |
| Linux | `lefalock_v0.0.15_install.sh` | amd64 / arm64 |
| macOS | `lefalock_v0.0.15_install.sh` | Intel / Apple Silicon |
| Windows | `lefalock_v0.0.15_install.ps1` | amd64 |

### Linux and macOS

Open a terminal and download the installer for this release:

```sh
release='https://github.com/Lefalock/clients-cli/releases/download'
version='v0.0.15'
installer="lefalock_${version}_install.sh"
curl --fail --location --proto '=https' --proto-redir '=https' \
  "$release/$version/$installer" --output "$installer"
sh "./$installer"
```

The installer chooses the platform archive automatically, checks its SHA256
against the same release's SHA256SUMS, and installs the executable and guide
in `~/.local/opt/lefalock`. It configures PATH for your login shell: bash,
zsh, sh, ksh, or fish. Bash login and interactive terminals are both covered;
zsh respects ZDOTDIR and fish respects XDG_CONFIG_HOME.

You need curl, tar, and either sha256sum or shasum. The installer reports a
missing prerequisite before installation. Your system credential store is
needed when you launch the application, not during installation.

### Windows PowerShell

Open PowerShell as your normal user:

```powershell
$release = 'https://github.com/Lefalock/clients-cli/releases/download'
$version = 'v0.0.15'
$installer = "lefalock_${version}_install.ps1"
Invoke-WebRequest -UseBasicParsing `
    -Uri "$release/$version/$installer" -OutFile $installer
powershell -NoProfile -ExecutionPolicy Bypass -File ".\$installer"
```

Requires Windows PowerShell 5.1 or later. The installer verifies the archive's
SHA256, installs the executable and guide in `%LOCALAPPDATA%\Programs\Lefalock`,
and adds that directory to your user's PATH. The execution-policy option applies
only to this installer process; it does not change your saved policy. On a
managed computer, administrator policy may still block script execution.

### Start Lefalock

For bash, zsh, or fish, open a **new terminal** after installation.
On Windows, fully close and reopen the terminal application if it keeps its
old environment.

For sh or ksh, apply PATH in your current shell:

`export PATH="$HOME/.local/opt/lefalock:$PATH"`

The installer saves that line in `.profile` for future login sessions.
An ordinary new terminal tab may not read `.profile`; either apply the line
above or start a new login session.

Check the installed version: `lefalock --version`.

Start the TUI from any directory: `lefalock`.

The installer cannot change the environment of its parent terminal. Use the
step for your shell above before running `lefalock` without its full path.

The installer adds PATH entries without removing existing entries. Repeating
installation does not add duplicate configuration lines. It replaces application
files only; your vault, credential-store entries, and backups are preserved.
For later application updates, use [the built-in updater](#updates).

If the OS blocks the executable, see [First-launch problems](#first-launch-problems).
If installation cannot download a release, check access to GitHub and that the
selected version is publicly available. Releases also include platform archives
and [source archives](#license-and-corresponding-source); Go is needed only
when building from source.

## Requirements

- The TUI needs a terminal of at least 60×18 characters; 80×24 or larger is recommended.
- Opening or creating a vault requires an accessible OS credential store:
  Secret Service on Linux, Keychain on macOS, or Credential Manager on Windows.
  Linux also requires a working user D-Bus session and an available secret service.
- Copying requires a system clipboard. On Linux, use `wl-copy`/`wl-paste` for
  Wayland, or `xclip`/`xsel` for X11. Clipboard and credential-store access may
  be unavailable over SSH or in a headless session.

Console commands using stdin, a file, or an inherited file descriptor do not
require a full-screen terminal. Help and the standalone password generator do
not require access to your vault or OS credential store.

## Everyday use

A **service** groups related credentials, for example a website named Mail.
A **secret** is an individual password or authenticator key within that service,
such as the password for your personal account. Notes can also live directly
in folders.

1. **Add another credential:** open a service and choose **Add secret**. Use
   **Path** to choose its location. To create another service, follow the
   same steps as in [Get started](#get-started).
2. **Find a record:** press `/` and search by a folder, service, or secret name.
   Search does not inspect secret values or note contents.
3. **Use a credential:** select it and choose **Copy**, or press `c` while the
   record list is focused. For an authenticator record, Copy gives the current
   one-time code. Open **Details** to see record information; **Show password
   (15s)** temporarily reveals a password, and **Show key (15s)** reveals the
   original authenticator key.
4. **Change a value:** select the record, choose **Edit**, enter the replacement,
   and save. Use **Rename** to change its name or **Move** to change its location.
5. **Back up your changes:** open **Backups → Create backup** and keep the new
   backup's recovery phrase separately. Older backups do not contain later edits.

Press Esc to close a dialog or go back. Press Ctrl+C to exit Lefalock.

### Navigation and records

| Action | Control |
| --- | --- |
| Search names | `/` |
| Open the selected object | Enter |
| Move between controls | Tab / Shift+Tab |
| Go back or close a dialog | Esc |
| Copy the selected value | `c` or Copy |
| Delete with confirmation | Delete or the Delete button |
| Focus section navigation | F6 |
| Open the command menu, including Settings | Ctrl+K |

Folders, services, and secrets can be renamed and moved. Only empty folders can
be deleted. Deleting a service also deletes its secrets.

**Edit** changes a password or TOTP key while preserving the record's ID and
name. The old value is revealed only on request. Save applies the change;
Cancel or Esc discards the input. A revealed value in the Edit dialog stays
visible until you hide it or close the dialog; there is no hiding timer there.

The password generator supports 4–128 characters and four character sets:
lowercase letters, uppercase letters, digits, and special characters. The
default is 20 characters with all sets enabled. Every selected set appears
in the generated password.

For TOTP, enter the original Base32 key. Lefalock generates six-digit SHA-1
codes with a 30-second period. If a code is rejected, check your computer's clock
and the service's TOTP settings.

### Notes

Select **Note** in Folders or Services. Choose a folder, Root, or a service in
**Place in**. A note stored in a folder does not need a service.

**Details** displays the full text, **Copy** copies it, and **Edit** opens the
editor. Enter inserts a newline; Ctrl+S or Save saves; Alt+Tab inserts a tab.
Cancel, Esc, or the close button discards changes. Use the on-screen buttons
if your terminal intercepts shortcuts.

Unicode, spaces, and line breaks are preserved. Empty notes are allowed.
Search uses titles, not note contents. Moving a note from a service into a folder
preserves its ID and text. Notes are included in backups and key rotation.

## Console commands

Close the TUI before running a command against the same vault: both use the
same process lock. Help is available without opening the vault.

```sh
lefalock vault --help
lefalock secret create --help
lefalock vault list
lefalock folder list
lefalock service list --query mail
lefalock secret list --service SERVICE_ID
lefalock note list
```

Replace `SERVICE_ID`, `SECRET_ID`, `NOTE_ID`, and `FOLDER_ID` with IDs returned
by a list command. Names may repeat, so changes use IDs. Lists and mutation
results use JSON without secret values. Diagnostics go to stderr; exit code 0
means success and 1 means an error.

```sh
# Enter a password at a hidden prompt, then confirm it:
lefalock service create --name Mail --secret-name alice

# Generate and save a password without displaying it:
lefalock secret create --service SERVICE_ID --name work --generate --length 32

lefalock folder create --name Work
lefalock service rename --id SERVICE_ID --name Email
lefalock service move --id SERVICE_ID --folder FOLDER_ID
lefalock secret set --id SECRET_ID --generate
lefalock secret rename --id SECRET_ID --name personal
lefalock secret delete --id SECRET_ID --yes
```

The `folder`, `service`, `secret`, and `note` groups support listing, creation,
renaming, moving, and deletion. `secret set` updates a value; `note set` updates
text. Specify move destinations explicitly: `--parent` for a folder, `--folder`
for a service or secret, and `--service` or `--folder` for a note. An empty folder
ID means Root. Use `--type auth_code` when creating a TOTP record.
Deletion, key rotation, and recovery changes require `--yes`.

## Handling secrets safely

Do not put passwords, TOTP keys, note contents, or recovery phrases in shell
command text: they may be saved in history or logs, or exposed in process lists.
Lefalock does not accept these values through command arguments or environment
variables. Avoid secret literals in `echo`, here-strings, and here-documents.
Disable shell tracing with `set +x` before handling secrets in POSIX shells.

Choose one input method:

- No input option: a hidden terminal prompt. A new password must be entered twice.
- `--value-stdin`: read stdin to EOF, for example directly from another program.
- `--value-file FILE`: read a regular private file. On Unix, it must have no
  group or other-user permissions, for example mode 0600.
- `--value-fd N`: read an inherited file descriptor, such as a pipe.
- `--generate`: generate a password directly in the vault.

Without a terminal, specify the source explicitly. Notes use stdin/file/FD and
preserve all spaces and line breaks. Passwords, keys, and recovery phrases have
one final LF or CRLF removed. Stream/file input is limited to 1 MiB.
For recovery phrases, use `--phrase-stdin`, `--phrase-file`, or `--phrase-fd`.
Supply only the 24 words, without a title or other text. For saved TUI phrase
files, see [Backups](#backups-and-moving-to-another-computer).

```sh
lefalock secret set --id SECRET_ID --value-file ./private/password
lefalock note create --name Memo --value-file ./private/note.txt
lefalock secret show --id SECRET_ID --output ./private/exported-password
lefalock secret code --id SECRET_ID --reveal
lefalock secret copy --id SECRET_ID
lefalock password generate --length 32 --output ./private/generated
```

Prepare input files securely before using these examples. The output directory
must already exist. `--output` creates a new file with mode 0600 on Unix and
refuses to overwrite an existing file. On Windows, directory ACLs determine
access; use a private directory.

`--reveal` explicitly writes the raw value to stdout without an extra newline.
Direct terminal output stays in scrollback; the TUI hiding timer does not apply.
`secret show` returns the password or **original TOTP key**; `secret code`
returns the current one-time code.

`secret copy` copies the password or current TOTP code; `note copy` copies the
note text. The command waits for conditional clipboard cleanup: 30 seconds by
default, configurable with options such as `--ttl 15s`. Ctrl+C clears early.
Content replaced by another application is preserved. Forced termination or
power loss prevents cleanup; clipboard-manager history is not cleared.

Use regular private files with `--value-file` and `--phrase-file`.
For pipes and other streams, use stdin or an inherited file descriptor.

## Backups and moving to another computer

In the TUI, open **Backups → Create backup** and choose a directory. Save the
24-word recovery phrase **separately** from the `.lbcp` file. Every backup
has its own phrase. **Save mnemonic to file** writes an unencrypted `-key.txt`
file containing the title `Lefalock backup recovery mnemonic`, a blank line,
and the 24 words. Protect that file like a password.

```sh
lefalock backup create --directory ./backups --phrase-output ./private/recovery.txt
```

The directories must exist. Without `--directory`, the profile's default backup
directory is used. `--phrase-output` is required and must name a new file.
The phrase is not printed in JSON. The result gives the actual backup path.
The CLI's `--phrase-output` file contains only the words and can be passed
directly to `--phrase-file` for backup restore or vault recovery.

To move to another computer, copy the backup, start Lefalock with an accessible
OS credential store, and choose **Backups → Restore backup**. Supply the file
and its phrase. Restore imports into the current vault and lets you resolve
name collisions by keeping the current record, replacing it, or importing
under a new name.

For a phrase saved through the TUI, copy only the 24 words into a separate
private file before using `--phrase-file`. Passing the entire `-key.txt` file,
including its title, produces `invalid recovery phrase`. On Unix, the new
file must have no group or other-user permissions (mode 0600); on Windows,
save it in a private directory. When entering a phrase in the TUI, also enter
only the words.

From the console, preview first and then apply:

```sh
lefalock backup preview --file ./backups/FILE.lbcp --phrase-file ./private/recovery.txt
lefalock backup restore --file ./backups/FILE.lbcp --phrase-file ./private/recovery.txt --yes
```

Replace `FILE.lbcp` with the real filename. If there are conflicts, prepare
a JSON decisions file and pass `--decisions FILE` to both commands. Use each
conflict's `Key` from the `conflicts` array:

```json
{
  "0:0": {"Choice": "keep-current"},
  "0:1": {"Choice": "replace"},
  "1:0": {"Choice": "keep-both", "NewName": "Imported account"},
  "2:0": {"Choice": "new-service", "NewServiceName": "Imported service"}
}
```

These keys illustrate the format; use keys from your own preview and provide
a decision for every conflict. Unresolved conflicts prevent import.

Copying the database alone is insufficient for migration: the vault key also
depends on the OS credential store. If the key is lost and you have no backup
with its recovery phrase, the old data cannot be recovered.

## When the vault will not open

If the OS credential store is unavailable, restore access in your OS session
and retry. Do not create an empty vault just because of a temporary credential
store failure.

If key material or vault metadata is lost or damaged, the TUI offers **Vault recovery**:

- **Restore backup** creates a separate vault with a new key. Old files are
  preserved. You need the backup, its phrase, and a working OS credential store.
- **Create empty vault → Create and open** creates a separate empty vault.
  Old files are preserved, but their data is not recovered by this action.

Console recovery:

```sh
lefalock recovery status
lefalock recovery begin --yes
lefalock recovery preview --file ./backups/FILE.lbcp --phrase-file ./private/recovery.txt
lefalock recovery restore --file ./backups/FILE.lbcp --phrase-file ./private/recovery.txt --yes
lefalock recovery activate --yes
```

After an interruption, check `recovery status`. If import has completed, continue
with activation. Use `recovery cancel --yes` to discard a safely cancellable
attempt. Use `recovery empty --yes` for a new empty vault, or
`recovery continue-empty --yes` to resume its interrupted creation.
If the prepared vault's pointer is damaged, use `recovery inspect-pointer`,
then `recovery repair-pointer --yes`, and finish activation.

## Storage, settings, and access

To see the actual storage paths and active profile: `lefalock config show`.

Data is stored in the Lefalock directory inside your OS user configuration
directory. The version reported by `--version` has the form `vX.Y.Z`.

Folder names, service names, secret names, and secret values are encrypted.
IDs, record types, relationships, and timestamps are not. The vault key has two
parts: a local key file and an OS credential-store entry. Do not delete either.

There is no separate master password or automatic vault lock. Access depends
on your OS user account and credential store. Lock your OS session when leaving
your computer unattended.

Settings offers vault key rotation.
The console equivalent is `lefalock vault rotate-key --yes`. Existing backups
and their recovery phrases remain usable after rotation.

## Updates

Releases are published in the public
[Lefalock/clients-cli repository](https://github.com/Lefalock/clients-cli/releases).
Built-in updates work without an account or access token:

```sh
lefalock update check
lefalock update
```

The updater verifies the checksum and replaces
the executable. On Unix, a system installation may request elevated permission
just for the replacement; do not run the entire command with sudo. On Windows,
replacement finishes after the updater process exits.

The application checks for updates automatically in the background; it does
not install them automatically. Run `lefalock update` to install an available
version. Installation replaces only the executable. Read the current guide in the
new release description or download its platform archive with the bundled README.
For a manual update, close Lefalock, download the archive for your platform,
and replace the executable in your installation directory. Keep the vault's
data directory.

## Troubleshooting

| Problem | What to check |
| --- | --- |
| Command not found | Follow [Start Lefalock](#start-lefalock) to activate PATH for your shell |
| Vault or credential-store error | Check access to your OS session and unlocked credential store; on Linux, also check user D-Bus |
| Another client is using the vault | Close the TUI or wait for the other CLI command, including copy, to finish |
| Terminal too small | Resize to at least 60×18 |
| Copy fails | Check clipboard availability; in headless sessions, use protected console output |
| Update unavailable | Check network access to GitHub and that a public stable release exists; retry later if rate limited |
| Backup will not restore | Check that the file and 24-word phrase match and the backup file is intact |

### First-launch problems

**Linux: the credential store is unavailable or locked**

1. Open a terminal in your normal desktop login session. The app needs that
   user's D-Bus session and a running Secret Service provider.
2. If no provider is installed, install and enable one using your distribution's
   desktop instructions; GNOME Keyring is one option. Installing a management
   application alone does not start the service.
3. With GNOME Keyring, open **Passwords and Keys** (Seahorse), select your
   default password keyring, and unlock it. See the
   [GNOME instructions](https://help.gnome.org/seahorse/keyring-unlock.html).
4. Retry Lefalock in the same user session. An SSH session or a container does
   not automatically inherit your desktop's credential store.

**Linux/macOS: permission denied**

Check the installed file's executable permission:

`chmod u+x "$HOME/.local/opt/lefalock/lefalock"`

If execution is still denied, check whether the installation location allows
executables. On macOS, an unidentified-developer warning is a separate issue.

**macOS: the developer cannot be verified**

macOS may block a downloaded application. After verifying the release's origin
and checksum, follow [Apple's instructions for opening downloaded apps](https://support.apple.com/en-ie/102445)
for your macOS version. A checksum match verifies the downloaded bytes; it is
not Apple notarization.

**Windows: SmartScreen blocks an unrecognized app**

Downloaded executables may trigger a reputation warning. Verify the release's
origin and checksum, then review the warning using
[Microsoft's explanation of SmartScreen](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).
On a managed computer, contact your administrator if policy blocks execution.

When requesting help, provide your version (`--version`), OS, architecture,
command name, and error message. Never send passwords, TOTP keys, recovery
phrases, key files, or access tokens. Review `config show` output before sharing:
it includes personal file paths.


## License and corresponding source

Lefalock's own CLI code is available under the Mozilla Public License 2.0
(MPL 2.0). This user guide is also available under MPL 2.0. Dependencies retain
their own licenses. Run the following without
opening a vault or accessing the network to read the complete license and
third-party notices, including the Go runtime and standard library:

`lefalock licenses`

The texts are embedded in the executable and remain available after an automatic
update. The source archives also contain LICENSE and THIRD_PARTY_NOTICES.txt.

This release is **v0.0.15**. Download its corresponding source archive:
[Lefalock v0.0.15 source](https://github.com/Lefalock/clients-cli/releases/download/v0.0.15/lefalock_v0.0.15_source.tar.gz).
The release also provides the same source as a ZIP and SHA256SUMS for all assets.
The source is publicly available without a token. If the link is unavailable,
ask the distributor for another copy. MPL rights in source you have already
received do not depend on continued server access.

### Corresponding third-party source

The macOS binaries include generated interface material derived from Apple
headers through modernc.org/libc v1.75.7 and modernc.org/sqlite v1.59.0. Those
Apple portions are available under the Apple Public Source License 2.0; the
original APSL 1.1 notice in the SQLite dependency is retained, and we elect the
later APSL 2.0 under section 7 of APSL 1.1.

Corresponding dependency source is publicly available:

- [modernc.org/libc v1.75.7 source](https://proxy.golang.org/modernc.org/libc/@v/v1.75.7.zip)
- [modernc.org/sqlite v1.59.0 source](https://proxy.golang.org/modernc.org/sqlite/@v/v1.59.0.zip)

The complete notices, license terms, module versions, and source checksums are
available with `lefalock licenses`. These third-party terms apply to their
respective portions; Lefalock's own code is licensed under MPL 2.0.

### Building the source

Extract the source archive and open a terminal in its directory. Install Go
**1.25.1** and allow Go to download the dependencies pinned by go.mod and go.sum.
No C compiler is needed. The following POSIX shell commands build the current
system's executable; choose `lefalock.exe` as the output name for Windows:

```sh
go mod download
go mod verify
release_version='v0.0.15'
release_base='https://github.com/Lefalock/clients-cli/releases/download'
source_archive="lefalock_${release_version}_source.tar.gz"
source_url="$release_base/$release_version/$source_archive"
CGO_ENABLED=0 go build -buildvcs=false -trimpath \
  -ldflags "-s -w \
    -X main.version=$release_version \
    -X lefalock/internal/legal.SourceURL=$source_url" \
  -o lefalock ./cmd/lefalock
./lefalock --version
./lefalock licenses
```

In PowerShell, use its continuation syntax:

```powershell
go mod download
go mod verify
$env:CGO_ENABLED = "0"
$releaseVersion = 'v0.0.15'
$releaseBase = 'https://github.com/Lefalock/clients-cli/releases/download'
$sourceArchive = "lefalock_${releaseVersion}_source.tar.gz"
$sourceUrl = "$releaseBase/$releaseVersion/$sourceArchive"
$buildFlags = @(
    '-s -w'
    "-X main.version=$releaseVersion"
    "-X lefalock/internal/legal.SourceURL=$sourceUrl"
) -join ' '
go build -buildvcs=false -trimpath `
    -ldflags $buildFlags -o lefalock.exe ./cmd/lefalock
.\lefalock.exe --version
.\lefalock.exe licenses
```

Use `GOOS` and `GOARCH` environment variables to cross-compile one of the five
platforms listed above. The binary's source link describes the original release;
when distributing modified source, update it to your corresponding source.
