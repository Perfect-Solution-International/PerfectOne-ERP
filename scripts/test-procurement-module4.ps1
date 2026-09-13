param([string]$BaseUrl = "http://127.0.0.1:18081")

$ErrorActionPreference = "Stop"

function Invoke-Api([string]$Method, [string]$Path, $Body = $null) {
  $params = @{ Method = $Method; Uri = "$BaseUrl$Path"; Headers = $script:Headers }
  if ($null -ne $Body) {
    $params.ContentType = "application/json"
    $params.Body = $Body | ConvertTo-Json -Depth 12
  }
  Invoke-RestMethod @params
}

function Expect-HttpError([int]$Status, [scriptblock]$Action, [string]$Message) {
  try {
    & $Action | Out-Null
    throw "Expected HTTP ${Status}: $Message"
  } catch {
    if ($_.Exception.Response.StatusCode.value__ -ne $Status) { throw }
  }
}

function Assert-True($Condition, [string]$Message) {
  if (-not $Condition) { throw "ASSERTION FAILED: $Message" }
}

$login = Invoke-RestMethod -Method Post -Uri "$BaseUrl/auth/login" -ContentType "application/json" -Body (@{
  identifier = "admin"
  password = "Admin@123"
} | ConvertTo-Json)
$script:Headers = @{ Authorization = "Bearer $($login.Token)" }
$suffix = [guid]::NewGuid().ToString("N").Substring(0, 8)

$suppliers = Invoke-Api GET "/suppliers"
$supplier = $null
$catalogue = @()
foreach ($candidate in $suppliers) {
  $items = Invoke-Api GET "/procurement/suppliers/$($candidate.id)/products"
  if (@($items).Count -gt 0) { $supplier = $candidate; $catalogue = @($items); break }
}
Assert-True ($null -ne $supplier) "A supplier with catalogue products is required"
$product = $catalogue[0]

$account = Invoke-Api POST "/cash-accounts" @{
  Name = "Procurement automated test $suffix"
  Type = "cash"
  openingBalance = 10000
  Description = "Disposable Module 4 integration test account"
}

$draftInvoice = "QA-DRAFT-$suffix"
$draft = Invoke-Api POST "/procurement/catalog-grns" @{
  supplierId = $supplier.id; invoiceNo = $draftInvoice; purchaseDate = (Get-Date).ToString("yyyy-MM-dd")
  paymentMethod = "credit"; paidAmount = 0; discount = 0; tax = 0; requestId = "draft-$suffix"
  items = @(@{ productId = $product.productId; quantity = 1; freeQuantity = 0; unitCost = $product.defaultPurchasePrice; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = $product.wholesalePrice; batchNo = "EDIT-$suffix-A"; manufacturedDate = ""; expiryDate = ""; discount = 0; tax = 0 })
}
$edited = Invoke-Api PUT "/procurement/catalog-grns/$($draft.id)" @{
  supplierId = $supplier.id; invoiceNo = $draftInvoice; purchaseDate = (Get-Date).ToString("yyyy-MM-dd")
  paymentMethod = "credit"; paidAmount = 0; discount = 0; tax = 0
  items = @(@{ productId = $product.productId; quantity = 2; freeQuantity = 0; unitCost = $product.defaultPurchasePrice; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = $product.wholesalePrice; batchNo = "EDIT-$suffix-B"; manufacturedDate = ""; expiryDate = ""; discount = 0; tax = 0 })
}
Assert-True ($edited.total -eq 2 * $product.defaultPurchasePrice) "Draft GRN edit must recalculate its total"
Invoke-Api POST "/procurement/catalog-grns/$($draft.id)/cancel" @{ reason = "Automated draft cancellation" } | Out-Null
$cancelled = Invoke-Api GET "/procurement/grns/$($draft.id)/detail"
Assert-True ($cancelled.status -eq "cancelled") "Draft cancellation must preserve a cancelled document"

Expect-HttpError 409 { Invoke-Api POST "/procurement/catalog-grns" @{
  supplierId = $supplier.id; invoiceNo = $draftInvoice; paymentMethod = "credit"; requestId = "duplicate-$suffix"
  items = @(@{ productId = $product.productId; quantity = 1; unitCost = $product.defaultPurchasePrice; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = 0 })
} } "Duplicate supplier invoice must be rejected"

Expect-HttpError 400 { Invoke-Api POST "/procurement/catalog-grns" @{
  supplierId = $supplier.id; invoiceNo = "QA-DATE-$suffix"; purchaseDate = (Get-Date).ToString("yyyy-MM-dd"); paymentMethod = "credit"; requestId = "dates-$suffix"
  items = @(@{ productId = $product.productId; quantity = 1; unitCost = $product.defaultPurchasePrice; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = 0; batchNo = "DATE-$suffix"; manufacturedDate = "2028-02-01"; expiryDate = "2028-01-01" })
} } "Manufacture date after expiry must be rejected"

$po = Invoke-Api POST "/purchase-orders" @{
  supplierId = $supplier.id; expectedDate = (Get-Date).AddDays(7).ToString("yyyy-MM-dd"); requestId = "po-$suffix"; discount = 0; tax = 0
  items = @(
    @{ productId = $product.productId; quantity = 3; unitCost = $product.defaultPurchasePrice; batchNo = "PO-$suffix-A"; discount = 0; tax = 0 },
    @{ productId = $product.productId; quantity = 2; unitCost = $product.defaultPurchasePrice; batchNo = "PO-$suffix-B"; discount = 0; tax = 0 }
  )
}
Invoke-Api POST "/purchase-orders/$($po.id)/approve" @{} | Out-Null
$poRow = (Invoke-Api GET "/purchase-orders") | Where-Object id -eq $po.id
$line = $poRow.items[0]

$reserved = Invoke-Api POST "/purchase-orders/$($po.id)/convert-grn" @{
  invoiceNo = "QA-RESERVE-$suffix"; paymentMethod = "credit"; paidAmount = 0; requestId = "reserve-$suffix"
  items = @(@{ purchaseOrderItemId = $line.id; quantity = 1; unitCost = $line.unitCost; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = $product.wholesalePrice; batchNo = "RES-$suffix" })
}
$poRow = (Invoke-Api GET "/purchase-orders") | Where-Object id -eq $po.id
Assert-True ($poRow.items[0].reserved -eq 1) "Draft PO receipt must reserve its quantity"
Invoke-Api POST "/procurement/catalog-grns/$($reserved.id)/cancel" @{ reason = "Release reservation test" } | Out-Null
$poRow = (Invoke-Api GET "/purchase-orders") | Where-Object id -eq $po.id
Assert-True ($poRow.items[0].reserved -eq 0) "Cancelling a draft receipt must release reserved quantity"
Assert-True ($poRow.status -eq "approved") "PO must return to approved when no receipt remains"

$receipt = Invoke-Api POST "/purchase-orders/$($po.id)/convert-grn" @{
  invoiceNo = "QA-FINAL-$suffix"; paymentMethod = "credit"; paidAmount = 0; requestId = "final-$suffix"
  items = @(@{ purchaseOrderItemId = $line.id; quantity = 2; unitCost = $line.unitCost; sellingPrice = $product.recommendedSellingPrice; wholesalePrice = $product.wholesalePrice; batchNo = "FINAL-$suffix" })
}
Invoke-Api POST "/procurement/catalog-grns/$($receipt.id)/finalize" @{ requestId = "finalize-$suffix" } | Out-Null
$receipts = Invoke-Api GET "/purchase-orders/$($po.id)/receipts"
Assert-True (@($receipts | Where-Object { $_.id -eq $receipt.id -and $_.status -eq "finalized" }).Count -eq 1) "PO receipt history must include the finalized GRN"

$grn = Invoke-Api GET "/procurement/grns/$($receipt.id)/detail"
$payment = Invoke-Api POST "/supplier-payments" @{
  partyId = $supplier.id; purchaseId = $receipt.id; amount = $grn.dueAmount; method = "cash"; accountId = $account.id
  date = (Get-Date).ToString("yyyy-MM-dd"); reference = "QA-PAY-$suffix"; notes = "Allocated integration test payment"; requestId = "payment-$suffix"
}
$history = Invoke-Api GET "/procurement/catalog-grns/$($receipt.id)/payments"
Assert-True ($history.due -eq 0) "Allocated supplier payment must settle the GRN"
Assert-True (@($history.payments).Count -eq 1) "GRN payment history must expose its allocation"
Expect-HttpError 409 { Invoke-Api POST "/procurement/catalog-grns/$($receipt.id)/reverse" @{ reason = "Must be blocked" } } "GRN reversal must be blocked while an allocated payment is active"
Invoke-Api POST "/supplier-payments/$($payment.id)/reverse" @{ reason = "Reverse allocation before GRN reversal" } | Out-Null
$history = Invoke-Api GET "/procurement/catalog-grns/$($receipt.id)/payments"
Assert-True ($history.due -eq $grn.total) "Reversing the supplier payment must reopen the GRN"
Invoke-Api POST "/procurement/catalog-grns/$($receipt.id)/reverse" @{ reason = "Automated stock/accounting reversal" } | Out-Null
$poRow = (Invoke-Api GET "/purchase-orders") | Where-Object id -eq $po.id
Assert-True ($poRow.status -eq "approved") "Reversing the only receipt must restore the approved PO state"

Write-Output "PASS Procurement Module 4 integration workflow ($suffix)"
