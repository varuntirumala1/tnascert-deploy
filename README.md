
# tnascert-deploy

A robust tool for deploying TLS certificates to TrueNAS SCALE hosts with intelligent duplicate prevention and smart renewal management.

## Features

- 🚀 **Smart Certificate Management**: Automatically detects existing certificates and prevents unnecessary duplicates
- ⏱️ **Intelligent Renewal**: Only deploys certificates when renewal is actually needed (30-day expiration window)
- 🔄 **Service Integration**: Seamlessly updates UI, FTP, and application certificates with minimal service disruption
- 🛡️ **Robust Error Handling**: Extended timeouts and comprehensive error recovery for reliable automated deployment
- 🎯 **Multi-Configuration Support**: Single config file can manage multiple TrueNAS hosts and certificate configurations
- 📊 **Comprehensive Logging**: Detailed debug output when needed, clean production logging by default

## Quick Start

```bash
# Basic certificate deployment
tnascert-deploy --config=my-cert.ini my_certificate

# Using default configuration section
tnascert-deploy --config=tnas-cert.ini default
```

## Synopsis

```bash
tnascert-deploy [OPTIONS] SECTION_NAME
```

### Options
- `-c, --config=PATH` - Full path to configuration file (default: ./tnas-cert.ini)
- `-h, --help` - Show help information
- `-v, --version` - Display version information

### Arguments
- `SECTION_NAME` - Configuration section name to use (default: "default")

## Description

tnascert-deploy is an advanced certificate deployment tool designed for TrueNAS SCALE hosts running **TrueNAS 25.04** or later. It provides intelligent certificate management with the following capabilities:

### Key Features

- **Smart Duplicate Prevention**: Automatically detects recently deployed certificates (within 30 minutes) and skips unnecessary deployments
- **Certificate Validity Checking**: Won't renew certificates that are still valid for more than 30 days, preventing excessive renewals
- **Extended Timeout Handling**: Uses appropriate timeouts for different operations (60+ seconds for app updates, shorter for API calls)
- **Multi-Service Support**: Can deploy certificates to UI, FTP services, and individual applications
- **Network Configuration Preservation**: Maintains existing app network settings during certificate updates
- **Robust Job Monitoring**: Real-time progress tracking with comprehensive error handling

### How It Works

1. **Certificate Analysis**: Scans existing certificates to determine if deployment is needed
2. **Smart Decision Making**: Only proceeds with deployment if:
   - No recent certificate exists (within 30 minutes), OR
   - Existing certificate is expiring within 30 days, OR
   - Applications aren't using the correct certificate
3. **Graceful Deployment**: Updates certificates with minimal service disruption
4. **Cleanup**: Optionally removes old certificates to prevent accumulation

### Configuration Management

The tool uses INI-style configuration files with multiple sections, allowing you to manage certificates for multiple TrueNAS hosts or different certificate types from a single configuration file. Each section represents a complete certificate deployment configuration.

## Configuration

### Configuration File Location

The default configuration file is `tnas-cert.ini` in the current working directory. You can specify an alternative file using the `--config` option with the full path.

### Configuration Settings

| Setting | Type | Description | Default |
|---------|------|-------------|---------|
| `api_key` | string | **Required** - TrueNAS 64-character API key | - |
| `cert_basename` | string | **Required** - Base name for certificate in TrueNAS | - |
| `connect_host` | string | **Required** - TrueNAS hostname or IP address | - |
| `full_chain_path` | string | **Required** - Path to certificate file (.crt/.pem) | - |
| `private_key_path` | string | **Required** - Path to private key file (.key) | - |
| `add_as_ui_certificate` | bool | Install as main UI certificate | false |
| `add_as_ftp_certificate` | bool | Install as FTP service certificate | false |
| `add_as_app_certificate` | bool | Install as application certificate | false |
| `app_name` | string | Application name (required if `add_as_app_certificate=true`) | - |
| `delete_old_certs` | bool | Remove old certificates after deployment | false |
| `port` | int | TrueNAS API port | 443 |
| `protocol` | string | WebSocket protocol ('ws' or 'wss') | wss |
| `tls_skip_verify` | bool | Skip SSL certificate verification | false |
| `timeoutSeconds` | int | API call timeout in seconds | 10 |
| `debug` | bool | Enable detailed debug logging | false |

### Sample Configuration

```ini
# Main TrueNAS UI certificate
[nas_ui_cert]
connect_host = "192.168.1.100"
api_key = "your-64-character-api-key-here"
cert_basename = "main_ui_cert"
full_chain_path = "/path/to/fullchain.pem"
private_key_path = "/path/to/privkey.pem"
add_as_ui_certificate = true
delete_old_certs = true
debug = false

# Application-specific certificate
[minio_cert]
connect_host = "192.168.1.100"
api_key = "your-64-character-api-key-here"
cert_basename = "minio_service"
full_chain_path = "/path/to/minio-fullchain.pem"
private_key_path = "/path/to/minio-privkey.pem"
add_as_app_certificate = true
app_name = "minio"
delete_old_certs = true
timeoutSeconds = 60
```

## Installation & Building

### Prerequisites

- Go 1.19 or later
- TrueNAS SCALE 25.04 or later
- Valid TrueNAS API key

### Build from Source

```bash
# Clone the repository
git clone https://github.com/varuntirumala1/tnascert-deploy.git
cd tnascert-deploy

# Build for current platform
go build -o tnascert-deploy .

# Cross-compile for Linux (if building on macOS/Windows)
GOOS=linux GOARCH=amd64 go build -o tnascert-deploy .
```

### Deploy to TrueNAS

```bash
# Copy binary to TrueNAS host
scp tnascert-deploy root@your-truenas-host:/usr/local/bin/
ssh root@your-truenas-host chmod +x /usr/local/bin/tnascert-deploy
```

## Usage Examples

### Basic Certificate Deployment

```bash
# Deploy a UI certificate
tnascert-deploy --config=ui-cert.ini main_ui

# Deploy an application certificate with debug output
tnascert-deploy --config=app-cert.ini minio_service
```

### Automated Certificate Renewal

The tool is designed for automated use with certificate renewal systems like Lego/ACME:

```bash
#!/bin/bash
# Certificate renewal script
if lego renew --days 30; then
    tnascert-deploy --config=production.ini web_certificate
fi
```

### Cron Job Integration

```bash
# Add to crontab for daily certificate checks
0 2 * * * /usr/local/bin/tnascert-deploy --config=/etc/ssl/tnas-cert.ini main_cert >> /var/log/cert-deploy.log 2>&1
```

## Smart Behavior

### Duplicate Prevention

The tool automatically prevents unnecessary certificate deployments:

- **Recent Certificate Check**: Won't deploy if an identical certificate was deployed within 30 minutes
- **Validity Check**: Won't renew certificates that are still valid for more than 30 days
- **Application Sync Check**: Won't update applications that are already using the correct certificate

### Error Recovery

- **Extended Timeouts**: App updates use 60+ second timeouts to accommodate service restart times
- **Graceful Degradation**: Continues processing other certificates if one fails (in multi-certificate scenarios)
- **Network Preservation**: Maintains existing application network configurations during certificate updates

## Troubleshooting

### Common Issues

1. **Timeout Errors**: If app updates fail with timeouts, increase `timeoutSeconds` in your configuration
2. **Permission Errors**: Ensure the TrueNAS API key has sufficient privileges for certificate management
3. **Certificate Conflicts**: Use `delete_old_certs = true` to automatically clean up old certificates

### Debug Mode

Enable detailed logging by setting `debug = true` in your configuration:

```ini
[debug_section]
debug = true
# ... other settings
```

This provides detailed information about:
- Certificate discovery and validation
- Application configuration analysis
- Job progress and timing
- Decision-making logic

## Technical Notes

This tool uses the TrueNAS SCALE JSON-RPC 2.0 API and WebSocket connections for real-time job monitoring. It supports TrueNAS SCALE 25.04 and later versions.

### API Compatibility

- **TrueNAS SCALE 25.04+**: Full feature support
- **WebSocket API**: Real-time job progress monitoring
- **JSON-RPC 2.0**: Standard API communication protocol

### Security Considerations

- API keys are transmitted over encrypted WebSocket connections (WSS)
- Certificate validation can be disabled for self-signed TrueNAS certificates using `tls_skip_verify = true`
- All certificate operations are performed with appropriate TrueNAS permissions

## Related Resources

- [TrueNAS API Client for Go](https://github.com/truenas/api_client_golang)
- [TrueNAS WebSocket API Documentation](https://www.truenas.com/docs/api/scale_websocket_api.html)
- [TrueNAS SCALE Documentation](https://www.truenas.com/docs/scale/)

## Contributing

Contributions are welcome! Please feel free to submit issues, feature requests, or pull requests.

## License

This project is open source. See the LICENSE file for details.

## Contact

**John J. Rushford**  
📧 jrushford@apache.org

---

*For automated certificate management workflows, consider pairing this tool with [Lego](https://go-acme.github.io/lego/) for ACME certificate acquisition.*
