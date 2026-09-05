param(
  [Parameter(Mandatory = $true)][string]$ArtifactPath,
  [Parameter(Mandatory = $true)][ValidatePattern("^[a-fA-F0-9]{64}$")][string]$ExpectedSha256,
  [Parameter(Mandatory = $true)][string]$ArtifactSignature,
  [Parameter(Mandatory = $true)][string]$SigningPublicKey,
  [Parameter(Mandatory = $true)][ValidatePattern("^[A-Za-z0-9._-]+$")][string]$SigningKeyId,
  [Parameter(Mandatory = $true)][string]$ConfigPath,
  [Parameter(Mandatory = $true)][string]$TrustBundlePath,
  [Parameter(Mandatory = $true)][ValidateRange(1, 9223372036854775807)][long]$TrustBundleEpoch,
  [Parameter(Mandatory = $true)][ValidatePattern("^[a-fA-F0-9]{64}$")][string]$TrustBundleSha256,
  [Parameter(Mandatory = $true)][string]$EnrollmentTokenPath
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
Set-StrictMode -Version Latest

function Assert-File([string]$Path, [string]$Purpose) {
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "$Purpose file is missing"
  }
}

function ConvertFrom-ArgusBase64([string]$Value) {
  $normalized = $Value.Trim().Replace("-", "+").Replace("_", "/")
  switch ($normalized.Length % 4) {
    2 { $normalized += "==" }
    3 { $normalized += "=" }
  }
  return [Convert]::FromBase64String($normalized)
}

function Assert-CABundle([string]$Path, [string]$ExpectedHash) {
  $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
  if ($actualHash -ne $ExpectedHash.ToLowerInvariant()) {
    throw "Trust Bundle SHA-256 mismatch"
  }
  $pem = [IO.File]::ReadAllText((Resolve-Path -LiteralPath $Path))
  $matches = [Text.RegularExpressions.Regex]::Matches(
    $pem,
    "-----BEGIN CERTIFICATE-----\s*(?<body>[A-Za-z0-9+/=\r\n]+)\s*-----END CERTIFICATE-----"
  )
  if ($matches.Count -eq 0) {
    throw "Trust Bundle contains no PEM certificate"
  }
  $remaining = [Text.RegularExpressions.Regex]::Replace(
    $pem,
    "-----BEGIN CERTIFICATE-----\s*[A-Za-z0-9+/=\r\n]+\s*-----END CERTIFICATE-----",
    ""
  )
  if (-not [String]::IsNullOrWhiteSpace($remaining)) {
    throw "Trust Bundle contains non-certificate data"
  }
  $seen = @{}
  $now = [DateTime]::UtcNow
  foreach ($match in $matches) {
    $raw = [Convert]::FromBase64String(($match.Groups["body"].Value -replace "\s", ""))
    $certificate = New-Object Security.Cryptography.X509Certificates.X509Certificate2 -ArgumentList @(,$raw)
    try {
      if ($now -lt $certificate.NotBefore.ToUniversalTime() -or $now -ge $certificate.NotAfter.ToUniversalTime()) {
        throw "Trust Bundle contains an expired or not-yet-valid CA"
      }
      $basic = $certificate.Extensions | Where-Object { $_.Oid.Value -eq "2.5.29.19" } | Select-Object -First 1
      if ($null -eq $basic) {
        throw "Trust Bundle contains a certificate without CA constraints"
      }
      $constraints = New-Object Security.Cryptography.X509Certificates.X509BasicConstraintsExtension
      $constraints.CopyFrom($basic)
      if (-not $constraints.CertificateAuthority) {
        throw "Trust Bundle contains a non-CA certificate"
      }
      $fingerprint = $certificate.GetCertHashString("SHA256").ToLowerInvariant()
      if ($seen.ContainsKey($fingerprint)) {
        throw "Trust Bundle contains a duplicate CA certificate"
      }
      $seen[$fingerprint] = $true
    } finally {
      $certificate.Dispose()
    }
  }
}

function Assert-ArtifactSignature(
  [string]$Path,
  [string]$Signature,
  [string]$PublicKey,
  [string]$TemporaryDirectory
) {
  $openssl = Get-Command "openssl.exe" -ErrorAction SilentlyContinue
  if ($null -eq $openssl) {
    throw "openssl.exe is required for Argus Ed25519 artifact verification"
  }
  $publicRaw = ConvertFrom-ArgusBase64 $PublicKey
  $signatureRaw = ConvertFrom-ArgusBase64 $Signature
  if ($publicRaw.Length -ne 32 -or $signatureRaw.Length -ne 64) {
    throw "Argus Ed25519 signing material has an invalid size"
  }
  [byte[]]$prefix = 0x30,0x2a,0x30,0x05,0x06,0x03,0x2b,0x65,0x70,0x03,0x21,0x00
  [IO.File]::WriteAllBytes((Join-Path $TemporaryDirectory "public.der"), $prefix + $publicRaw)
  [IO.File]::WriteAllBytes((Join-Path $TemporaryDirectory "artifact.sig"), $signatureRaw)
  & $openssl.Source pkey -pubin -inform DER -in (Join-Path $TemporaryDirectory "public.der") -out (Join-Path $TemporaryDirectory "public.pem") 2>$null
  if ($LASTEXITCODE -ne 0) { throw "Argus Ed25519 public key was rejected" }
  & $openssl.Source dgst -sha256 -binary -out (Join-Path $TemporaryDirectory "artifact.hash") $Path 2>$null
  if ($LASTEXITCODE -ne 0) { throw "Collector artifact hashing failed" }
  & $openssl.Source pkeyutl -verify -pubin -inkey (Join-Path $TemporaryDirectory "public.pem") -rawin -in (Join-Path $TemporaryDirectory "artifact.hash") -sigfile (Join-Path $TemporaryDirectory "artifact.sig") 2>$null
  if ($LASTEXITCODE -ne 0) { throw "Collector artifact Ed25519 signature verification failed" }
}

Assert-File $ArtifactPath "Collector artifact"
Assert-File $ConfigPath "Collector configuration"
Assert-File $TrustBundlePath "Argus Trust Bundle"
Assert-File $EnrollmentTokenPath "Collector enrollment token"

$artifactHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArtifactPath).Hash.ToLowerInvariant()
if ($artifactHash -ne $ExpectedSha256.ToLowerInvariant()) {
  throw "Collector artifact SHA-256 mismatch"
}
Assert-CABundle $TrustBundlePath $TrustBundleSha256
$token = [IO.File]::ReadAllText((Resolve-Path -LiteralPath $EnrollmentTokenPath)).Trim()
if ([String]::IsNullOrWhiteSpace($token)) {
  throw "Collector enrollment token is empty"
}

$temporary = Join-Path ([IO.Path]::GetTempPath()) ("argus-otelcol-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
  Assert-ArtifactSignature $ArtifactPath $ArtifactSignature $SigningPublicKey $temporary
  Expand-Archive -LiteralPath $ArtifactPath -DestinationPath (Join-Path $temporary "release")
  $verifiedBinary = Join-Path $temporary "release\argus-otelcol.exe"
  Assert-File $verifiedBinary "Verified Collector binary"

  # No privileged filesystem or service mutation occurs before every supplied
  # artifact and public trust input has passed validation.
  $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
  if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Windows system installation requires an elevated PowerShell after verification"
  }

  $installRoot = Join-Path $env:ProgramFiles "Argus\otelcol"
  $stateRoot = Join-Path $env:ProgramData "Argus\otelcol"
  $identityRoot = Join-Path $stateRoot "identity"
  New-Item -ItemType Directory -Force -Path $installRoot, $stateRoot, $identityRoot | Out-Null

  $effectiveConfig = [IO.File]::ReadAllText((Resolve-Path -LiteralPath $ConfigPath))
  $effectiveConfig = $effectiveConfig.Replace("/etc/argus-otelcol/enrollment-token", (Join-Path $stateRoot "enrollment-token").Replace("\", "/"))
  $effectiveConfig = $effectiveConfig.Replace("/etc/argus-otelcol/server-ca.pem", (Join-Path $stateRoot "server-ca.pem").Replace("\", "/"))
  $effectiveConfig = $effectiveConfig.Replace("/var/lib/argus-otelcol/identity", $identityRoot.Replace("\", "/"))
  $null = $effectiveConfig | ConvertFrom-Json

  $service = Get-Service -Name "ArgusOtelcol" -ErrorAction SilentlyContinue
  if ($null -ne $service -and $service.Status -ne "Stopped") {
    Stop-Service -Name "ArgusOtelcol" -Force
  }
  Copy-Item -Force -LiteralPath $verifiedBinary -Destination (Join-Path $installRoot "argus-otelcol.exe")
  [IO.File]::WriteAllText((Join-Path $stateRoot "config.yaml"), $effectiveConfig, (New-Object Text.UTF8Encoding($false)))
  Copy-Item -Force -LiteralPath $TrustBundlePath -Destination (Join-Path $stateRoot "server-ca.pem")
  [IO.File]::WriteAllText((Join-Path $stateRoot "enrollment-token"), $token, (New-Object Text.UTF8Encoding($false)))
  $state = @{
    schema_version = "argus.collector_state/v3"
    trust_bundle_epoch = $TrustBundleEpoch
    trust_bundle_sha256 = $TrustBundleSha256.ToLowerInvariant()
    signing_key_id = $SigningKeyId
    updated_at = [DateTime]::UtcNow.ToString("o")
  } | ConvertTo-Json -Compress
  [IO.File]::WriteAllText((Join-Path $stateRoot "state.json"), $state, (New-Object Text.UTF8Encoding($false)))

  $binary = Join-Path $installRoot "argus-otelcol.exe"
  $serviceCommand = "`"$binary`" --config `"$(Join-Path $stateRoot 'config.yaml')`""
  if ($null -eq $service) {
    sc.exe create ArgusOtelcol binPath= $serviceCommand start= auto | Out-Null
  } else {
    sc.exe config ArgusOtelcol binPath= $serviceCommand start= auto | Out-Null
  }
  if ($LASTEXITCODE -ne 0) { throw "Argus Collector service configuration failed" }
  sc.exe failure ArgusOtelcol reset= 86400 actions= restart/5000/restart/15000/restart/60000 | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Argus Collector recovery policy failed" }
  Start-Service -Name "ArgusOtelcol"
  $service = Get-Service -Name "ArgusOtelcol"
  $service.WaitForStatus("Running", [TimeSpan]::FromSeconds(30))
} finally {
  Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
  $token = $null
}
