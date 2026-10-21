# Security

## Reporting a vulnerability

Email **satyajit.rajaraman@halotrcer.com** with "halotrcer security" in the subject, or use GitHub's private vulnerability reporting on this repository (the Security tab, "Report a vulnerability"). Please do not open a public issue for a vulnerability.

Include what you can of:
- the halotrcer version (`halotrcer version`), how it was installed, and the Tokvera and Sievant versions `halotrcer doctor` reports;
- the command and flags, or the input, that triggers it;
- what happens, and what you expected.

Do not send real API keys, MCP configuration files with their secrets, or client data: a reproduction with made-up values is enough.

We aim to acknowledge reports within 5 business days, and to tell you what happens next.

A vulnerability in Tokvera or Sievant themselves belongs to that project: see [Tokvera's](https://github.com/rajasatyajit/tokvera/blob/main/SECURITY.md) and [Sievant's](https://github.com/rajasatyajit/sievant/blob/main/SECURITY.md) security policies. If you're not sure which project it is, write to the address above.

## Supported versions

Halotrcer is before 1.0. Fixes go into the next release; there are no maintained older release lines. `halotrcer update --check` tells you whether a newer release exists.

## What a release proves

- **Signature:** every release's `checksums.txt` is signed with cosign, keyless, by this repository's release workflow running on a version tag. The install scripts and `halotrcer install` check that signature when cosign is installed, and always check the SHA-256 of what they download against `checksums.txt`. They check each tool against its own repository's release workflow in the same way. `--require-cosign` refuses to install without the signature check.
- **Provenance:** every archive, package and SBOM, the container image and the Helm chart carry a SLSA v1 build provenance attestation from the same workflow, built on GitHub-hosted runners.
- **Container image:** signed with cosign by the same workflow. It carries the three binaries copied from Tokvera's and Sievant's own signed images, each verified against its release workflow before it is used.
- **Packages:** deb and rpm packages, and the apt and yum repository metadata, are signed with the shared key in [packaging/keys](packaging/keys). The apk is signed with Halotrcer's own key, published there too.
- **SBOMs:** each archive ships with one.

[docs/install.md](docs/install.md) has the commands to check each of these yourself.

## What halotrcer does with your data

- **No telemetry.** Halotrcer contacts only the target you give it (through Tokvera) and reads only the MCP configuration files Sievant reads, on this machine. It sends no telemetry. Secrets stay masked as each tool masks them; on top of that, Halotrcer drops URL credentials and query strings, hides environment values, shows your home directory as ~ and replaces your user name with <user>.
- **Installing and updating** contact GitHub, to find and download releases (`install`, `update`, `update --check`), or hand the work to the package manager that owns a tool.
- **API keys** are read by Tokvera from the environment variable you name with `--api-key-env`. Halotrcer passes that variable's name and never its value, and refuses a value given where a name belongs. Each tool gets only the environment variables it documents.
- **Before any load is sent,** you see Tokvera's own plan and confirm it, or pass `--yes`.
- **Sievant reads, never writes:** `halotrcer check` runs `sievant scan`, which starts no MCP server and changes no configuration.
- **Tests plant canaries,** such as a key, a token in a URL, an environment value, the home path and the user name, in the tools' outputs and the environment, and check that none reaches any report, log or error.
