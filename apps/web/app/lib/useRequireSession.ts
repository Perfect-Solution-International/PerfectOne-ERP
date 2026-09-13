"use client";
import{useEffect}from"react";
import{useRouter}from"next/navigation";
import{getSession}from"./api";
export function useRequireSession(){const router=useRouter();useEffect(()=>{if(!getSession())router.replace("/login")},[router])}
