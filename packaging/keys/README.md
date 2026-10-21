# Public signing keys

Public halves only. The private keys live in GitHub Actions secrets and never
in this repository (`TestPublicSigningKeysAreCommitted` refuses private key
material here, and gitleaks runs in `make secrets` and CI).

| File | Signs | Identity |
| --- | --- | --- |
| `satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub` | Halotrcer's apk packages (RSA-4096; secret `PKG_APK_KEY`), Halotrcer's own key (owner, 2026-10-21) | install as `/etc/apk/keys/satyajit.rajaraman@halotrcer.com-halotrcer.rsa.pub`; sha256 of the DER public key `8fd0bd739a0b08e5a1b38016ff2b709f9312a0399bf60b465d4ee16841de15c4` |
| `halotrcer-packages.asc` | deb/rpm packages and the apt/yum repositories (OpenPGP; secret `PKG_GPG_KEY`), shared with Tokvera and Sievant, which publish to the same `rajasatyajit/packages` repositories; byte-identical to their copies | `Halotrcer Packages <satyajit.rajaraman@halotrcer.com>`, RSA-4096, fingerprint `D2D9 6618 831A C55B 81E2  4DA3 039B 5DA0 D3E3 9071` |

`packaging/repo-meta.env` is the shared repositories' settings seed, byte-identical
to Tokvera's and Sievant's (PLAN.md D7).
