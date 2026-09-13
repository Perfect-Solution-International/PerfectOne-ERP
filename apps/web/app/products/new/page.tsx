"use client";
import {useRouter,useSearchParams} from "next/navigation";
import Sidebar from "../../components/Sidebar";
import ProductForm from "../../components/ProductForm";
import {apiFetch} from "../../lib/api";
import {useRequireSession} from "../../lib/useRequireSession";

export default function NewProduct(){
 useRequireSession();const router=useRouter(),params=useSearchParams(),supplierId=params.get("supplierId")||"";
 return <main className="shell"><Sidebar active="Products"/><section className="app"><header className="topbar"><div><span className="crumb">Stock &amp; products / Products</span><b>Add product</b></div></header><div className="page productFormPage"><div className="pageHeading"><div><p>PRODUCT LIST</p><h1>Create product</h1><small>{supplierId?"Create this product and automatically add it to the selected supplier.":"Add product identity, pricing, stock and sales information."}</small></div></div><ProductForm onSave={async value=>{const created=await apiFetch("/products",{method:"POST",body:JSON.stringify(value)});if(supplierId&&created?.id)await apiFetch(`/procurement/suppliers/${supplierId}/products`,{method:"POST",body:JSON.stringify({productId:created.id,defaultPurchasePrice:Number(value.purchasePrice||0),recommendedSellingPrice:Number(value.sellingPrice||0),wholesalePrice:Number(value.wholesalePrice||0),minimumOrderQty:1,packSize:1,leadTimeDays:0,isPreferred:true,isActive:true})});router.push(supplierId?"/suppliers?view=ledger":"/products")}}/></div></section></main>
}
