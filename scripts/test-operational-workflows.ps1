param([string]$BaseUrl = "http://127.0.0.1:8080")
$ErrorActionPreference = "Stop"
function Api([string]$Method,[string]$Path,$Body=$null){$p=@{Method=$Method;Uri="$BaseUrl$Path";Headers=$script:Headers};if($null-ne$Body){$p.ContentType="application/json";$p.Body=$Body|ConvertTo-Json -Depth 10};Invoke-RestMethod @p}
function Assert($Value,[string]$Message){if(-not$Value){throw "ASSERTION FAILED: $Message"}}
$login=Invoke-RestMethod -Method Post -Uri "$BaseUrl/auth/login" -ContentType application/json -Body (@{identifier="admin";password="Admin@123"}|ConvertTo-Json)
$script:Headers=@{Authorization="Bearer $($login.Token)"}
Assert ($login.Token) "Admin login must return a token"
Assert ($null-ne(Api GET "/dashboard?period=today")) "Dashboard must load"
Assert ($null-ne(Api GET "/reports/inventory")) "Inventory report must load"
Assert ($null-ne(Api GET "/reports/customer-payments")) "Finance report must load"
Assert ($null-ne(Api GET "/reports/trial-balance")) "Trial Balance must load"
Assert ($null-ne(Api GET "/accounting/statements/profit-loss")) "Profit and Loss must load"
Assert ($null-ne(Api GET "/accounting/statements/balance-sheet")) "Balance Sheet must load"
Assert ($null-ne(Api GET "/inventory/movements")) "Immutable stock movements must load"
Assert ($null-ne(Api GET "/inventory/transfers")) "Stock transfers must load"
Assert ($null-ne(Api GET "/pos/discount-approvals")) "Discount approval queue must load"
Write-Host "PASS: authentication, dashboard, reports, accounting, inventory and POS approval smoke tests"
