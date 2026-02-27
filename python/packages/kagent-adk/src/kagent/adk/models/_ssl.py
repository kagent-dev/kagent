"""SSL/TLS utilities for configuring httpx clients with custom certificates."""

import logging
import ssl
from datetime import datetime, timezone
from pathlib import Path

logger = logging.getLogger(__name__)


def get_ssl_troubleshooting_message(
    error: Exception, ca_cert_path: str | None = None, server_url: str | None = None
) -> str:
    """Generate actionable troubleshooting message for SSL errors.

    Args:
        error: The original SSL error.
        ca_cert_path: Path to custom CA certificate if one was configured.
        server_url: URL of the server that was being accessed.

    Returns:
        Formatted troubleshooting message with specific debugging steps.
    """
    troubleshooting_steps = [
        "\n" + "=" * 70,
        "SSL/TLS Connection Error",
        "=" * 70,
        f"Error: {error}",
        "",
        "Troubleshooting Steps:",
        "",
    ]

    if ca_cert_path:
        troubleshooting_steps.extend(
            [
                "1. Verify the CA certificate is correctly mounted:",
                f"   kubectl exec <pod-name> -- cat {ca_cert_path}",
                "",
                "2. Inspect the certificate details:",
                f"   kubectl exec <pod-name> -- openssl x509 -in {ca_cert_path} -text -noout",
                "",
                "3. Check the certificate validity period:",
                f"   kubectl exec <pod-name> -- openssl x509 -in {ca_cert_path} -noout -dates",
                "",
            ]
        )

    if server_url:
        troubleshooting_steps.extend(
            [
                "4. Test the server certificate chain:",
                f"   openssl s_client -connect {server_url} -showcerts",
                "",
                "5. Verify the server certificate is signed by your CA:",
                f"   openssl s_client -connect {server_url} -CAfile {ca_cert_path or '<ca-file>'} -verify 5",
                "",
            ]
        )

    troubleshooting_steps.extend(
        [
            "6. Check Kubernetes Secret contents:",
            "   kubectl get secret <secret-name> -o yaml",
            "   # Verify the certificate data is base64-encoded PEM format",
            "",
            "7. Verify the ModelConfig TLS configuration:",
            "   kubectl get modelconfig <name> -o yaml",
            "   # Check spec.tls.caCertSecretRef and spec.tls.caCertSecretKey",
            "",
            "For more information, see:",
            "   https://kagent.dev/docs",
            "=" * 70,
        ]
    )

    return "\n".join(troubleshooting_steps)


def validate_certificate(cert_path: str) -> None:
    """Validate certificate format and log metadata (warnings only, non-blocking).

    This function attempts to parse the certificate file and log useful metadata
    including subject, serial number, and validity period. Validation issues are
    logged as warnings but do not prevent the certificate from being loaded.

    Args:
        cert_path: Path to the certificate file in PEM format.

    Note:
        This function requires the 'cryptography' library. If not available,
        validation is skipped with an info log message.
    """
    try:
        from cryptography import x509
        from cryptography.hazmat.backends import default_backend
    except ImportError:
        logger.info(
            "cryptography library not available - skipping certificate validation. "
            "Install with: pip install cryptography"
        )
        return

    try:
        with open(cert_path, "rb") as f:
            cert_data = f.read()
        cert = x509.load_pem_x509_certificate(cert_data, default_backend())

        # Log certificate metadata
        logger.info("Certificate subject: %s", cert.subject.rfc4514_string())
        logger.info("Certificate serial number: %s", hex(cert.serial_number))
        logger.info(
            "Certificate valid from %s to %s",
            cert.not_valid_before_utc,
            cert.not_valid_after_utc,
        )

        # Warn about expiry (non-blocking)
        now = datetime.now(timezone.utc)
        if cert.not_valid_after_utc < now:
            logger.warning(
                "Certificate has EXPIRED on %s. Please update the certificate Secret.",
                cert.not_valid_after_utc,
            )
        elif cert.not_valid_before_utc > now:
            logger.warning(
                "Certificate is not yet valid until %s. Check system clock or certificate validity period.",
                cert.not_valid_before_utc,
            )

    except Exception as e:
        logger.warning(
            "Could not validate certificate format at %s: %s. Certificate will still be loaded, but may be invalid.",
            cert_path,
            e,
        )


def create_ssl_context(
    disable_verify: bool,
    ca_cert_path: str | None,
    disable_system_cas: bool,
) -> ssl.SSLContext | bool:
    """Create SSL context for httpx client based on TLS configuration.

    This function creates an appropriate SSL context based on three possible modes:
    1. Verification disabled: Returns False (httpx accepts False to disable verification)
    2. Custom CA only: Creates SSL context with custom CA certificate, no system CAs
    3. System + Custom CA: Creates SSL context with system CAs plus custom CA certificate

    Args:
        disable_verify: If True, SSL verification is disabled (development/testing only).
            When True, a prominent warning is logged.
        ca_cert_path: Optional path to custom CA certificate file in PEM format.
            If provided, the certificate is loaded into the SSL context.
        disable_system_cas: If True, system CA certificates are NOT included in the trust store.
            When False (default), system CAs are used (safe behavior).
            When True with ca_cert_path, only the custom CA is trusted.

    Returns:
        - False if disable_verify=True (httpx special value to disable verification)
        - ssl.SSLContext configured with appropriate CA certificates otherwise

    Raises:
        FileNotFoundError: If ca_cert_path is provided but file does not exist.
        ssl.SSLError: If certificate file is invalid or cannot be loaded.

    Examples:
        >>> # Disable verification (development only)
        >>> ctx = create_ssl_context(disable_verify=True, ca_cert_path=None, disable_system_cas=False)
        >>> assert ctx is False

        >>> # Use only custom CA certificate
        >>> ctx = create_ssl_context(
        ...     disable_verify=False, ca_cert_path="/etc/ssl/certs/custom/ca.crt", disable_system_cas=True
        ... )
        >>> assert isinstance(ctx, ssl.SSLContext)

        >>> # Use system CAs plus custom CA
        >>> ctx = create_ssl_context(
        ...     disable_verify=False, ca_cert_path="/etc/ssl/certs/custom/ca.crt", disable_system_cas=False
        ... )
        >>> assert isinstance(ctx, ssl.SSLContext)
    """
    # Structured logging for TLS configuration at startup
    if disable_verify:
        logger.warning(
            "\n"
            "=" * 60 + "\n"
            "⚠️  SSL VERIFICATION DISABLED ⚠️\n"
            "=" * 60 + "\n"
            "SSL certificate verification is disabled.\n"
            "This should ONLY be used in development/testing.\n"
            "Production deployments MUST use proper certificates.\n"
            "=" * 60
        )
        logger.info("TLS Mode: Disabled (disable_verify=True)")
        return False  # httpx accepts False to disable verification

    # Determine TLS mode
    if ca_cert_path and not disable_system_cas:
        tls_mode = "Custom CA + System CAs (additive)"
    elif ca_cert_path:
        tls_mode = "Custom CA only (no system CAs)"
    else:
        tls_mode = "System CAs only (default)"

    logger.info("TLS Mode: %s", tls_mode)

    # Start with system CAs or empty context
    if not disable_system_cas:
        # Create default context which includes system CAs
        ctx = ssl.create_default_context()
        logger.info("Using system CA certificates")
    else:
        # Create empty context without system CAs
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.check_hostname = True
        ctx.verify_mode = ssl.CERT_REQUIRED
        logger.info("System CA certificates disabled (disable_system_cas=True)")

    # Load custom CA certificate if provided
    if ca_cert_path:
        cert_path = Path(ca_cert_path)
        if not cert_path.exists():
            raise FileNotFoundError(
                f"CA certificate file not found: {ca_cert_path}\n"
                f"Please ensure the certificate Secret is mounted correctly.\n"
                f"Check: kubectl get secret <secret-name> -n <namespace>"
            )

        # Validate certificate format and log metadata
        validate_certificate(str(cert_path))

        try:
            ctx.load_verify_locations(cafile=str(cert_path))
            logger.info("Custom CA certificate loaded from: %s", ca_cert_path)
        except ssl.SSLError as e:
            raise ssl.SSLError(
                f"Failed to load CA certificate from {ca_cert_path}: {e}\n"
                f"Please verify the certificate is in valid PEM format.\n"
                f"You can inspect it with: openssl x509 -in {ca_cert_path} -text -noout"
            ) from e

    return ctx
