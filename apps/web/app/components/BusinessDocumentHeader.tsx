"use client";
import"./BusinessDocumentHeader.css";
import{useEffect,useState}from"react";import{apiFetch}from"../lib/api";
export default function BusinessDocumentHeader(){const[x,setX]=useState<any>(null);useEffect(()=>{apiFetch("/settings").then(setX).catch(()=>{})},[]);return <div className="businessDocumentBrand">{x?.showBusinessLogo&&x.logoUrl&&<img src={x.logoUrl} alt=""/>}<div><b>{x?.businessName||"Business"}</b>{x?.showBusinessAddress&&x.address&&<small>{x.address}</small>}{x?.showBusinessContact&&(x.phone||x.email)&&<small>{[x.phone,x.email].filter(Boolean).join(" · ")}</small>}{x?.taxRegistrationNo&&<small>Tax: {x.taxRegistrationNo}</small>}</div></div>}
