# E2E Test Suite - ModelConfig TLS Support

## Overview

This directory contains end-to-end tests for ModelConfig TLS support, verifying actual TLS connections with self-signed certificates.

## Test Files

### Test Certificates (`tests/fixtures/certs/`)

Self-signed certificates for testing TLS connections:
- `ca-cert.pem` - Test Certificate Authority certificate
- `ca-key.pem` - CA private key
- `server-cert.pem` - Server certificate signed by test CA
- `server-key.pem` - Server private key
- `README.md` - Certificate generation instructions

**Certificate Details:**
- CA Common Name: Test CA
- Server Common Name: localhost
- Subject Alternative Names: DNS:localhost, IP:127.0.0.1
- Validity: 365 days
- Key Size: RSA 4096 bits

### Test Suites

- `test_ssl.py` - Unit tests for SSL context creation
- `test_tls_e2e.py` - End-to-end tests with actual HTTPS server
- `test_tls_integration.py` - Integration tests for TLS configuration
- `test_openai.py` - OpenAI client TLS configuration tests

## Running the Tests

### Prerequisites

- Python 3.11+ (required by kagent-adk)
- All dependencies installed: `pip install -e .`

### Run All TLS Tests

```bash
cd /path/to/kagent/python/packages/kagent-adk
pytest tests/unittests/models/test_ssl.py -v
pytest tests/unittests/models/test_tls_e2e.py -v
pytest tests/unittests/models/test_tls_integration.py -v
```

### Run Specific Test

```bash
pytest tests/unittests/models/test_tls_e2e.py::test_e2e_with_self_signed_cert -v -s
```

### Run with Coverage

```bash
pytest tests/unittests/models/ --cov=kagent.adk.models._ssl --cov-report=term-missing
```

## What Tests Verify

The test suite covers:

- **SSL context creation** with custom CAs, system CAs, or verification disabled
- **TLS handshakes** with self-signed certificates
- **Connection failures** when proper CA is not provided (negative testing)
- **OpenAI SDK integration** with custom TLS configuration
- **Connection pooling** with custom SSL contexts
- **Warning logs** when verification is disabled
- **Backward compatibility** with no TLS configuration
- **Error message quality** for troubleshooting

### TLS Modes Tested

1. **Custom CA Only** - Uses only provided CA certificate
2. **System + Custom CA** - Additive behavior with both CA types
3. **Verification Disabled** - Development mode (not recommended for production)
4. **Default Behavior** - Backward compatible, uses system CAs

## Test Infrastructure

The test suite includes:

- `TestHTTPSServer` - Background HTTPS server for realistic testing
- `MockLLMHandler` - OpenAI-compatible mock responses
- Self-signed test certificates in `tests/fixtures/certs/`

## Certificate Validation

To verify test certificates are valid:

```bash
cd tests/fixtures/certs
openssl x509 -in ca-cert.pem -text -noout
openssl verify -CAfile ca-cert.pem server-cert.pem
```
