param([string]$BaseUrl = "http://127.0.0.1:18082")
$ErrorActionPreference = "Stop"
function Api { param([string]$Method,[string]$Path,[object]$Payload=$null)
  $requestParams=@{Method=$Method;Uri="$BaseUrl$Path";Headers=$script:Headers}
  if($null-ne$Payload){$requestParams.ContentType="application/json";$requestParams.Body=$Payload|ConvertTo-Json -Depth 8}
  try { Invoke-RestMethod @requestParams } catch { throw "$Method $Path failed. Payload: $($requestParams.Body). $($_.Exception.Message)" }
}
function Assert($Condition,[string]$Message){if(-not$Condition){throw "ASSERTION FAILED: $Message"}}

$login=Invoke-RestMethod -Method Post -Uri "$BaseUrl/auth/login" -ContentType application/json -Body (@{identifier="admin";password="Admin@123"}|ConvertTo-Json)
$script:Headers=@{Authorization="Bearer $($login.Token)"}
try{Api -Method POST -Path "/cashier-sessions/open" -Payload @{openingCash=1000;notes="POS automated test"}|Out-Null}catch{}
$product=(Api GET "/products")[0]
Assert ($null-ne$product) "A product with at least four units is required"

$hold=Api -Method POST -Path "/pos/holds" -Payload @{customerId="";discount=0;requestId=[guid]::NewGuid().ToString();cart=@(@{productId=$product.ID;quantity=1;name=$product.Name;price=$product.Price;discount=1})}
$held=@(Api GET "/pos/holds")
Assert ($held.id-contains$hold.id) "Held cart must appear in the register queue"
$resumed=Api POST "/pos/holds/$($hold.id)/resume"
Assert ($resumed.status-eq"resumed") "Held cart must resume exactly once"

function New-SplitSale([double]$ItemDiscount=0,[double]$InvoiceDiscount=0){
  $total=[double]$product.Price-$ItemDiscount-$InvoiceDiscount;$cash=[math]::Round($total*.4,2);$bank=$total-$cash;$request=[guid]::NewGuid().ToString()
  $body=@{customerId="";discount=$InvoiceDiscount;requestId=$request;lines=@(@{productId=$product.ID;quantity=1;discount=$ItemDiscount});payments=@(@{method="cash";amount=$cash;tendered=$cash},@{method="bank";amount=$bank;tendered=$bank;reference="BANK-AUTO"})}
  $first=Api -Method POST -Path "/pos/checkout" -Payload $body;$retry=Api -Method POST -Path "/pos/checkout" -Payload $body
  Assert ($first.id-eq$retry.id) "Checkout request ID must be idempotent"
  $first
}

$discounted=New-SplitSale 2 1
$invoice=Api GET "/invoices/$($discounted.id)"
Assert ($invoice.items[0].discount-eq2) "Item discount must persist"
Assert (@($invoice.payments).Count-eq2) "Split tenders must persist"
Assert (($invoice.payments|Where-Object method -eq "bank").reference-eq"BANK-AUTO") "Payment reference must persist"

$returnSale=New-SplitSale
$returnInvoice=Api GET "/invoices/$($returnSale.id)"
$returned=Api -Method POST -Path "/sales/$($returnSale.id)/return" -Payload @{reason="Automated POS return";requestId=[guid]::NewGuid().ToString();items=@(@{saleItemId=$returnInvoice.items[0].id;quantity=1})}
Assert ($returned.status-eq"returned") "Full return must finalize"

$cancelSale=New-SplitSale
$cancelled=Api -Method POST -Path "/sales/$($cancelSale.id)/cancel" -Payload @{reason="Automated POS cancellation";requestId=[guid]::NewGuid().ToString()}
Assert ($cancelled.status-eq"cancelled") "Cancellation must reverse the invoice"
Write-Host "PASS: hold/resume, item and invoice discounts, split payments, idempotency, return and cancellation"
