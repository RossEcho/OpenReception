$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$environmentFile = Join-Path $projectRoot '.env'
$settings = @{}

Get-Content -LiteralPath $environmentFile | ForEach-Object {
    if ($_ -match '^([^#=]+)=(.*)$') {
        $settings[$matches[1].Trim()] = $matches[2].Trim()
    }
}

$port = if ($settings.WEBHOOK_PORT) { $settings.WEBHOOK_PORT } else { '3000' }
$path = if ($settings.WEBHOOK_PATH) { $settings.WEBHOOK_PATH } else { '/webhook/whatsapp' }
$token = $settings.WEBHOOK_VERIFY_TOKEN
$baseUrl = "http://127.0.0.1:$port"

$health = Invoke-RestMethod -Method Get -Uri "$baseUrl/health"
if ($health.status -ne 'ok') {
    throw 'Health check did not return status=ok'
}

$challenge = 'openreception-local-challenge'
$encodedToken = [Uri]::EscapeDataString($token)
$verification = Invoke-WebRequest -UseBasicParsing -Method Get -Uri "$baseUrl$path`?hub.mode=subscribe&hub.verify_token=$encodedToken&hub.challenge=$challenge"
if ($verification.StatusCode -ne 200 -or $verification.Content -ne $challenge) {
    throw 'Webhook verification test failed'
}

$payload = @{
    object = 'whatsapp_business_account'
    entry = @()
} | ConvertTo-Json -Depth 10

$requestHeaders = @{}
if ($settings.META_APP_SECRET) {
    $secretBytes = [Text.Encoding]::UTF8.GetBytes($settings.META_APP_SECRET)
    $payloadBytes = [Text.Encoding]::UTF8.GetBytes($payload)
    $hmac = [Security.Cryptography.HMACSHA256]::new($secretBytes)
    try {
        $signatureBytes = $hmac.ComputeHash($payloadBytes)
        $signature = ($signatureBytes | ForEach-Object { $_.ToString('x2') }) -join ''
        $requestHeaders['X-Hub-Signature-256'] = "sha256=$signature"
    }
    finally {
        $hmac.Dispose()
    }
}

$delivery = Invoke-WebRequest -UseBasicParsing -Method Post -Uri "$baseUrl$path" -ContentType 'application/json' -Headers $requestHeaders -Body $payload
if ($delivery.StatusCode -ne 200 -or $delivery.Content -ne 'EVENT_RECEIVED') {
    throw 'Webhook delivery test failed'
}

Write-Host 'Local webhook tests passed: health, verification GET, and event POST.'
