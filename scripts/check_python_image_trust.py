"""Check the active Python image trust stores against the eMudhra compromise."""

import hashlib
import os
import re
import ssl
import sys
import sysconfig
import unittest
import zipfile
from importlib import import_module
from importlib.util import find_spec
from pathlib import Path

import botocore.httpsession
import certifi
import grpc

# These roots authenticate the compromised intermediates, including cross-signs.
# https://emudhra.we-have-a-problem.com/roots.json
BLOCKED_ROOTS = {
    "7dd78d5f4f13459a83dff9abbba62edbaf6f2d102bf257fd712f4d9f2746ed8d",
    "f2edaaff3d9b701fb4d14c9f285f8c0bad90fc3bcc1fa07d47f4718edaba34a5",
    "d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4",
    "86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b",
}


def bundle_fingerprints(bundle):
    return {
        hashlib.sha256(ssl.PEM_cert_to_DER_cert(pem)).hexdigest()
        for pem in re.findall(
            r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----",
            bundle,
            re.DOTALL,
        )
    }


class ImageTrustTest(unittest.TestCase):
    def assert_safe_bundle(self, path):
        roots = bundle_fingerprints(Path(path).read_text())
        self.assertTrue(roots, f"Empty CA bundle: {path}")
        self.assertFalse(roots & BLOCKED_ROOTS, f"Compromised CA trust in {path}")

    def test_system_roots(self):
        paths = ssl.get_default_verify_paths()
        roots = {
            hashlib.sha256(root).hexdigest()
            for root in ssl.create_default_context().get_ca_certs(binary_form=True)
        }
        # OpenSSL loads hashed-directory certificates lazily during verification.
        if paths.capath:
            for path in Path(paths.capath).iterdir():
                if path.is_file():
                    roots.update(bundle_fingerprints(path.read_text()))
        self.assertTrue(roots, "The Python default trust store is empty")
        self.assertFalse(roots & BLOCKED_ROOTS)

    def test_library_bundles(self):
        paths = [
            certifi.where(),
            botocore.httpsession.get_cert_path(True),
            Path(botocore.httpsession.__file__).with_name("cacert.pem"),
        ]
        if find_spec("httplib2") is not None:
            paths.append(import_module("httplib2.certs").where())
        for path in paths:
            with self.subTest(bundle=path):
                self.assert_safe_bundle(path)

    def test_grpc_roots(self):
        # Python gRPC's roots callback precedes its system-store fallback.
        path = os.environ.get("GRPC_DEFAULT_SSL_ROOTS_FILE_PATH")
        self.assertTrue(path, "gRPC must use the updated system CA bundle")
        self.assert_safe_bundle(path)
        self.assertIsNotNone(grpc.ssl_channel_credentials())

    def test_managed_python(self):
        self.assertGreaterEqual(sys.version_info[:3], (3, 13, 17))

    def test_ensurepip_bundles(self):
        bundled = Path(sysconfig.get_path("stdlib")) / "ensurepip" / "_bundled"
        for wheel in bundled.glob("pip-*.whl"):
            with self.subTest(wheel=wheel), zipfile.ZipFile(wheel) as archive:
                roots = bundle_fingerprints(
                    archive.read("pip/_vendor/certifi/cacert.pem").decode()
                )
                self.assertTrue(roots)
                self.assertFalse(
                    roots & BLOCKED_ROOTS, f"Compromised CA trust in {wheel}"
                )


if __name__ == "__main__":
    unittest.main(verbosity=2)
