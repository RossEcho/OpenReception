param(
    [Parameter(Mandatory = $true)]
    [string]$WabaId,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v\d+\.\d+$')]
    [string]$GraphApiVersion
)

$ErrorActionPreference = 'Stop'

$secureToken = Read-Host 'Paste the Meta access token (input is hidden)' -AsSecureString
$tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)

try {
    $accessToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer)
    $headers = @{ Authorization = "Bearer $accessToken" }
    $uri = "https://graph.facebook.com/$GraphApiVersion/$WabaId/subscribed_apps"

    Write-Host 'Current app subscriptions:'
    $before = Invoke-RestMethod -Method Get -Uri $uri -Headers $headers
    $before | ConvertTo-Json -Depth 10

    $answer = Read-Host 'Subscribe the app associated with this token to the WABA now? (y/N)'
    if ($answer -notin @('y', 'Y')) {
        Write-Host 'No changes made.'
        exit 0
    }

    $result = Invoke-RestMethod -Method Post -Uri $uri -Headers $headers
    Write-Host 'Subscription result:'
    $result | ConvertTo-Json -Depth 10

    Write-Host 'Updated app subscriptions:'
    Invoke-RestMethod -Method Get -Uri $uri -Headers $headers | ConvertTo-Json -Depth 10
}
finally {
    if ($tokenPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer)
    }
    Remove-Variable accessToken -ErrorAction SilentlyContinue
}
