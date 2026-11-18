# Smoke test for SnapRQ's MCP server. Run it with SnapRQ open and MCP enabled:
#
#   $env:SNAPRQ_MCP_TOKEN = "<token from Settings > MCP server>"
#   ./scripts/mcp-smoke.ps1            # lists tools and your projects
#   ./scripts/mcp-smoke.ps1 -Port 47821
#
# It only reads: initialize, tools/list, list_projects.
param([int]$Port = 47821)

$token = $env:SNAPRQ_MCP_TOKEN
if (-not $token) { Write-Error "Set SNAPRQ_MCP_TOKEN first."; exit 1 }
$url = "http://127.0.0.1:$Port/mcp"
$base = @{ "Content-Type" = "application/json"; "Accept" = "application/json, text/event-stream" }

function Send($headers, $body) {
  $r = Invoke-WebRequest -Uri $url -Method Post -Headers $headers -Body ($body | ConvertTo-Json -Depth 10 -Compress) -UseBasicParsing
  $text = $r.Content
  if ($text -match "(?m)^data:") {
    $text = (($text -split "`n") | Where-Object { $_ -like "data:*" } | ForEach-Object { $_.Substring(5).Trim() }) -join "`n"
  }
  return @{ Headers = $r.Headers; Json = if ($text) { $text | ConvertFrom-Json } else { $null } }
}

Write-Host "1. Without a token (expect 401)"
try {
  Invoke-WebRequest -Uri $url -Method Post -Headers $base -Body "{}" -UseBasicParsing | Out-Null
  Write-Host "   UNEXPECTED: the server answered without a token" -ForegroundColor Red
} catch {
  Write-Host ("   {0}" -f $_.Exception.Response.StatusCode.value__)
}

$auth = $base + @{ "Authorization" = "Bearer $token" }

Write-Host "2. initialize"
$init = Send $auth @{ jsonrpc = "2.0"; id = 1; method = "initialize"; params = @{
  protocolVersion = "2025-03-26"; capabilities = @{}; clientInfo = @{ name = "snaprq-smoke"; version = "0" } } }
Write-Host ("   server: {0} {1}" -f $init.Json.result.serverInfo.name, $init.Json.result.serverInfo.version)
$session = $init.Headers["Mcp-Session-Id"]
if ($session) { $auth["Mcp-Session-Id"] = "$session" }
Send $auth @{ jsonrpc = "2.0"; method = "notifications/initialized" } | Out-Null

Write-Host "3. tools/list"
$tools = Send $auth @{ jsonrpc = "2.0"; id = 2; method = "tools/list" }
$names = $tools.Json.result.tools | ForEach-Object { $_.name }
Write-Host ("   {0} tools: {1}" -f $names.Count, ($names -join ", "))

Write-Host "4. list_projects"
$call = Send $auth @{ jsonrpc = "2.0"; id = 3; method = "tools/call"; params = @{ name = "list_projects"; arguments = @{} } }
Write-Host ("   " + $call.Json.result.content[0].text)
