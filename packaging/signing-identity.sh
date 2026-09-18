#!/usr/bin/env bash
# Print the name of a code-signing identity whose signature macOS TCC keeps
# honouring across rebuilds, creating it in the login keychain on first use.
# Prints nothing and exits 0 when no identity can be had, which tells the
# caller to fall back to ad-hoc signing.
#
# Why this exists: ad-hoc signing (`codesign --sign -`) has no certificate to
# anchor identity to, so the designated requirement macOS stores alongside an
# Accessibility or Microphone grant is the code hash itself. Every rebuild
# changes that hash, the grant stops matching, and Vox prompts for permission
# again while System Settings still lists it as enabled. A certificate makes
# the requirement "this bundle ID, signed by this certificate", which holds
# no matter how often the binary changes.
#
# The certificate is self-signed and stays untrusted. Signing only needs the
# private key and the codeSigning extended key usage; TCC matches the leaf
# certificate hash without consulting the trust store. Adding it there would
# cost an admin prompt and buy nothing for a locally built bundle.
set -euo pipefail

IDENTITY_NAME="${VOX_SIGN_IDENTITY_NAME:-Vox Dev}"
KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

# Apple's PKCS#12 reader rejects what OpenSSL 3 writes by default ("Unknown
# format") and chokes on empty-password bundles, so the certificate is always
# built with the system LibreSSL and a throwaway password.
SYSTEM_OPENSSL=/usr/bin/openssl
P12_PASSWORD=vox-dev-import

# Matches on all code-signing identities, not `-v` valid ones: a self-signed
# certificate that was never added to the trust store is reported invalid
# (CSSMERR_TP_NOT_TRUSTED) yet signs perfectly well.
have_identity() {
	security find-identity -p codesigning 2>/dev/null |
		grep -Fq "\"$IDENTITY_NAME\""
}

if have_identity; then
	printf '%s\n' "$IDENTITY_NAME"
	exit 0
fi

# A missing keychain or openssl is not fatal: the caller signs ad-hoc instead.
if [ ! -f "$KEYCHAIN" ] || [ ! -x "$SYSTEM_OPENSSL" ]; then
	echo "signing-identity: no login keychain or system openssl; falling back to ad-hoc" >&2
	exit 0
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# codeSigning EKU is what makes codesign and `find-identity -p codesigning`
# accept the certificate; CA:FALSE keeps it a leaf.
cat >"$WORKDIR/openssl.cnf" <<EOF
[req]
distinguished_name = dn
prompt = no
[dn]
CN = $IDENTITY_NAME
[ext]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
EOF

if ! "$SYSTEM_OPENSSL" req -x509 -newkey rsa:2048 -nodes -days 3650 -sha256 \
	-keyout "$WORKDIR/key.pem" -out "$WORKDIR/cert.pem" \
	-config "$WORKDIR/openssl.cnf" -extensions ext >/dev/null 2>&1; then
	echo "signing-identity: could not generate certificate; falling back to ad-hoc" >&2
	exit 0
fi

if ! "$SYSTEM_OPENSSL" pkcs12 -export -inkey "$WORKDIR/key.pem" \
	-in "$WORKDIR/cert.pem" -name "$IDENTITY_NAME" \
	-passout "pass:$P12_PASSWORD" -out "$WORKDIR/identity.p12" >/dev/null 2>&1; then
	echo "signing-identity: could not package certificate; falling back to ad-hoc" >&2
	exit 0
fi

# -A lets any program on this machine use the private key. Without it codesign
# raises a GUI keychain prompt on every build and unattended `make app` hangs.
# The key signs nothing but local dev bundles.
if ! security import "$WORKDIR/identity.p12" -k "$KEYCHAIN" -P "$P12_PASSWORD" \
	-T /usr/bin/codesign -A >/dev/null 2>&1; then
	echo "signing-identity: could not import identity; falling back to ad-hoc" >&2
	exit 0
fi

if ! have_identity; then
	echo "signing-identity: imported identity is not usable for code signing; falling back to ad-hoc" >&2
	exit 0
fi

echo "signing-identity: created \"$IDENTITY_NAME\" in the login keychain." >&2
echo "signing-identity: grant Accessibility once more; the grant now survives rebuilds." >&2
printf '%s\n' "$IDENTITY_NAME"
