"use client";
import Image from "next/image";
import {FormEvent,KeyboardEvent,useEffect,useRef,useState} from "react";
import {useRouter} from "next/navigation";
import "./login.css";

type IconName="shieldProtected"|"lockEncrypted"|"layers"|"person"|"building"|"monitor"|"shieldCheck"|"eye"|"eyeOff"|"globe"|"briefcase"|"userLine"|"lockLine";
function Icon({name}:{name:IconName}){
 if(name==="eye"||name==="eyeOff"||name==="userLine"||name==="lockLine"){
  const strokePaths:Record<string,React.ReactNode>={
   eye:<><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z"/><circle cx="12" cy="12" r="3"/></>,
   eyeOff:<><path d="M3.5 3.5l17 17M9.9 5.5A9.7 9.7 0 0 1 12 5.3c6 0 9.5 6.7 9.5 6.7a17 17 0 0 1-3.4 4.3M6.6 6.9A16.6 16.6 0 0 0 2.5 12S6 18.7 12 18.7a10.6 10.6 0 0 0 3.3-.5M14.2 14.2a3 3 0 0 1-4.2-4.2"/></>,
   userLine:<><circle cx="12" cy="8" r="3.6"/><path d="M4.8 20c0-4 3.3-6.2 7.2-6.2S19.2 16 19.2 20"/></>,
   lockLine:<><rect x="5" y="10.5" width="14" height="9.5" rx="2.2"/><path d="M8 10.5V7.8a4 4 0 0 1 8 0v2.7"/></>,
  };
  return <svg viewBox="0 0 24 24"><g fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">{strokePaths[name]}</g></svg>;
 }
 const fillPaths:Record<string,string>={
  shieldProtected:"M12 2 4 5v6c0 5.05 3.41 9.73 8 11 4.59-1.27 8-5.95 8-11V5l-8-3Zm0 17.9c-3.08-1.1-5.5-4.87-5.5-8.9V6.62L12 4.56l5.5 2.06V11c0 4.03-2.42 7.8-5.5 8.9Z",
  lockEncrypted:"M17 8V6a5 5 0 0 0-10 0v2H5v14h14V8h-2Zm-8-2a3 3 0 0 1 6 0v2H9V6Zm8 14H7V10h10v10Z",
  layers:"m12 2 9 5-9 5-9-5 9-5Zm-7 9.5 7 3.89 7-3.89V15l-7 3.89L5 15v-3.5Zm0 7 7 3.89 7-3.89V21l-7 3.89L5 21v-2.5Z",
  person:"M12 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10Zm0 2c-5.33 0-8 2.67-8 6v2h16v-2c0-3.33-2.67-6-8-6Z",
  building:"M4 21V3h10v6h6v12H4Zm3-3h2v-2H7v2Zm0-4h2v-2H7v2Zm0-4h2V8H7v2Zm0-4h2V5H7v1Zm5 12h2v-2h-2v2Zm0-4h2v-2h-2v2Zm0-4h2V8h-2v2Zm5 8h1v-6h-1v6Z",
  monitor:"M4 5h16v11H4V5Zm-2 13h20v2H2v-2Z",
  shieldCheck:"M12 2 4 5v6c0 5.05 3.41 9.73 8 11 4.59-1.27 8-5.95 8-11V5l-8-3Zm-1 14-4-4 1.41-1.41L11 13.17l4.59-4.58L17 10l-6 6Z",
  globe:"M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm6.93 6H15.5a17 17 0 0 0-1.14-4.3A8.03 8.03 0 0 1 18.93 8ZM12 4.06c.83 1.1 1.5 2.5 1.93 3.94h-3.86c.43-1.44 1.1-2.84 1.93-3.94ZM4.26 14a8.14 8.14 0 0 1 0-4h3.82a17.6 17.6 0 0 0 0 4H4.26Zm.81 2h3.29c.24 1.53.65 2.99 1.14 4.3A8.03 8.03 0 0 1 5.07 16Zm3.29-8H5.07a8.03 8.03 0 0 1 4.43-4.3A17 17 0 0 0 8.36 8ZM12 19.94c-.83-1.1-1.5-2.5-1.93-3.94h3.86c-.43 1.44-1.1 2.84-1.93 3.94ZM14.36 14H9.64a15.8 15.8 0 0 1 0-4h4.72a15.8 15.8 0 0 1 0 4Zm.14 6.3c.49-1.31.9-2.77 1.14-4.3h3.29a8.03 8.03 0 0 1-4.43 4.3ZM15.86 14a17.6 17.6 0 0 0 0-4h3.82a8.14 8.14 0 0 1 0 4h-3.82Z",
  briefcase:"M9 3h6a2 2 0 0 1 2 2v2h3a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2h3V5a2 2 0 0 1 2-2Zm0 4h6V5H9v2Zm-5 4v7h16v-7H4Zm7 1h2v2h-2v-2Z",
 };
 return <svg viewBox="0 0 24 24"><path d={fillPaths[name]} fill="currentColor"/></svg>;
}

const USERNAME_KEY="perfectone_remembered_username";

export default function Login(){
 const[identifier,setIdentifier]=useState(""),[password,setPassword]=useState(""),[remember,setRemember]=useState(false),[showPassword,setShowPassword]=useState(false),[capsLock,setCapsLock]=useState(false),[error,setError]=useState(""),[loading,setLoading]=useState(false),router=useRouter();
 const cardRef=useRef<HTMLDivElement|null>(null);
 const ambientOneRef=useRef<HTMLDivElement|null>(null);
 const ambientTwoRef=useRef<HTMLDivElement|null>(null);
 const glowRef=useRef<HTMLDivElement|null>(null);
 const glowTarget=useRef({x:0,y:0});
 const glowCurrent=useRef({x:0,y:0});

 useEffect(()=>{const saved=localStorage.getItem(USERNAME_KEY);if(saved){setIdentifier(saved);setRemember(true)}},[]);

 // Smooth cursor-follow glow: eases toward the pointer every frame for a
 // gliding, premium feel instead of snapping straight to the mouse.
 useEffect(()=>{
  let raf=0;
  const tick=()=>{
   const c=glowCurrent.current,t=glowTarget.current;
   c.x+=(t.x-c.x)*.12;c.y+=(t.y-c.y)*.12;
   if(glowRef.current)glowRef.current.style.transform=`translate3d(${c.x}px, ${c.y}px, 0)`;
   raf=requestAnimationFrame(tick);
  };
  raf=requestAnimationFrame(tick);
  return ()=>cancelAnimationFrame(raf);
 },[]);

 // Card tilt + cursor spotlight, premium "glare card" effect.
 const tiltCard=(event:React.MouseEvent<HTMLDivElement>)=>{
  const el=cardRef.current;if(!el)return;
  const rect=el.getBoundingClientRect();
  const px=(event.clientX-rect.left)/rect.width,py=(event.clientY-rect.top)/rect.height;
  el.style.setProperty("--mx",`${(px*100).toFixed(1)}%`);
  el.style.setProperty("--my",`${(py*100).toFixed(1)}%`);
  el.style.setProperty("--ry",`${((px-.5)*10).toFixed(2)}deg`);
  el.style.setProperty("--rx",`${((.5-py)*8).toFixed(2)}deg`);
 };
 const resetCard=()=>{const el=cardRef.current;if(!el)return;el.style.setProperty("--rx","0deg");el.style.setProperty("--ry","0deg")};

 // Subtle whole-page parallax on the ambient glows.
 const trackPage=(event:React.MouseEvent<HTMLElement>)=>{
  const w=window.innerWidth,h=window.innerHeight;
  const dx=(event.clientX/w-.5),dy=(event.clientY/h-.5);
  if(ambientOneRef.current){ambientOneRef.current.style.marginLeft=`${dx*-30}px`;ambientOneRef.current.style.marginTop=`${dy*-24}px`}
  if(ambientTwoRef.current){ambientTwoRef.current.style.marginLeft=`${dx*22}px`;ambientTwoRef.current.style.marginTop=`${dy*18}px`}
  glowTarget.current={x:event.clientX,y:event.clientY};
 };

 async function submit(event:FormEvent){
  event.preventDefault();if(loading)return;setLoading(true);setError("");
  try{
   const response=await fetch("/api/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({identifier:identifier.trim(),password})});
   if(!response.ok)throw new Error();
   const data=await response.json();
   localStorage.setItem("grocerly_session",JSON.stringify(data));
   if(remember)localStorage.setItem(USERNAME_KEY,identifier.trim());else localStorage.removeItem(USERNAME_KEY);
   router.replace(data.Permissions?.includes("pos")&&!data.Permissions?.includes("dashboard")?"/pos":"/");
  }catch{setError("We couldn't sign you in. Check your username and password, then try again.")}
  finally{setLoading(false)}
 }
 const checkCaps=(event:KeyboardEvent<HTMLInputElement>)=>setCapsLock(event.getModifierState("CapsLock"));

 return <main className="pfShell" onMouseMove={trackPage}>
  <div className="pfCursorGlow" ref={glowRef} aria-hidden="true"/>
  <div className="pfAmbient pfAmbientOne" ref={ambientOneRef}/><div className="pfAmbient pfAmbientTwo" ref={ambientTwoRef}/><div className="pfGridOverlay"/>

  <div className="pfTopbar">
   <div className="pfMicroBrand"><span>ENTERPRISE</span><span>BEYOND</span><span>TOMORROW</span><i/></div>
   <div className="pfTopMessage">PEOPLE <b>/</b> PROCESS <b>/</b> INTELLIGENCE <b>/</b> A BETTER TOMORROW</div>
  </div>

  <section className="pfGrid">
   <aside className="pfLeft">
    <div className="pfHeadline">
     <p className="pfEyebrow">PERFECT SOLUTION INTERNATIONAL</p>
     <h1>Business<br/><span className="pfShine">without limits.</span></h1>
     <p className="pfLead">Intelligence today.<br/>A stronger tomorrow.</p>
     <div className="pfAccentLine"/>
    </div>

    <div className="pfNetwork" aria-hidden="true">
     <div className="pfPlanet">
      <span className="pfOrbit"/><span className="pfOrbit pfOrbitB"/><span className="pfOrbit pfOrbitC"/>
      <span className="pfNode pfN1"/><span className="pfNode pfN2"/><span className="pfNode pfN3"/>
      <span className="pfNode pfN4"/><span className="pfNode pfN5"/><span className="pfNode pfN6"/>
     </div>
    </div>

    <div className="pfStats">
     <div><span className="pfStatIcon"><Icon name="globe"/></span><div><strong>195+</strong><span>COUNTRIES READY</span></div></div>
     <div><span className="pfStatIcon"><Icon name="briefcase"/></span><div><strong>10K+</strong><span>BUSINESS ENTITIES</span></div></div>
     <div><span className="pfStatIcon"><Icon name="layers"/></span><div><strong>1 PLATFORM</strong><span>ONE OPERATING SYSTEM</span></div></div>
    </div>
   </aside>

   <section className="pfZone">
    <div className="pfCard" ref={cardRef} onMouseMove={tiltCard} onMouseLeave={resetCard}>
     <div className="pfCardGlow"/>
     <div className="pfSpotlight" aria-hidden="true"/>

     <div className="pfLogoWrap">
      <Image src="/perfectone-mark.png" width={1922} height={818} alt="PerfectOne ERP" className="pfLogo" priority/>
      <p>INTELLIGENT BUSINESS OPERATING SYSTEM</p>
     </div>

     <div className="pfDivider"><span/><i/><span/></div>

     <div className="pfWelcome">
      <p className="pfKicker">SECURE ENTERPRISE ACCESS</p>
      <h2>Welcome back</h2>
      <p>Sign in with your workspace account.</p>
     </div>

     <form className="pfCredentials pfOpen" onSubmit={submit}>
      <label>
       <span>Username or email</span>
       <div className="pfFieldIcon">
        <span className="pfFieldIconGlyph"><Icon name="userLine"/></span>
        <input type="text" value={identifier} onChange={e=>setIdentifier(e.target.value)} autoComplete="username" placeholder="Enter username or email" autoFocus required/>
       </div>
      </label>
      <label>
       <span>Password</span>
       <div className="pfFieldIcon pfPasswordField">
        <span className="pfFieldIconGlyph"><Icon name="lockLine"/></span>
        <input type={showPassword?"text":"password"} value={password} onChange={e=>setPassword(e.target.value)} onKeyUp={checkCaps} onKeyDown={checkCaps} autoComplete="current-password" placeholder="Enter your password" required/>
        <button type="button" className="pfEyeToggle" onClick={()=>setShowPassword(v=>!v)} aria-label={showPassword?"Hide password":"Show password"}><Icon name={showPassword?"eyeOff":"eye"}/></button>
       </div>
       {capsLock&&<small className="pfCapsWarning">Caps Lock is on</small>}
      </label>
      <div className="pfFormRow">
       <label className="pfRemember"><input type="checkbox" checked={remember} onChange={e=>setRemember(e.target.checked)}/><span>Trust this device</span></label>
       <a href="mailto:info@perfectsolutioninternational.com?subject=PerfectOne%20ERP%20Password%20Support">Forgot password?</a>
      </div>
      {error&&<div className="pfFormError" role="alert">{error}</div>}
      <button className="pfBtn pfBtnLogin" type="submit" disabled={loading}>{loading?<><i/> Signing in…</>:<>Sign in securely<em className="pfArrow">→</em></>}</button>
     </form>

     <div className="pfSecurityRow">
      <div><Icon name="shieldProtected"/><span>Protected</span></div>
      <div><Icon name="lockEncrypted"/><span>Encrypted</span></div>
      <div><Icon name="layers"/><span>Adaptive Access</span></div>
     </div>
    </div>
   </section>

   <aside className="pfRight">
    <div className="pfStatusStack">
     <div className="pfStatusCard"><span className="pfStatusIcon"><Icon name="person"/></span><div><small>Access profile</small><strong>Enterprise User</strong></div></div>
     <div className="pfStatusCard"><span className="pfStatusIcon"><Icon name="building"/></span><div><small>Workspace</small><strong>PerfectOne Cloud</strong></div></div>
     <div className="pfStatusCard"><span className="pfStatusIcon"><Icon name="monitor"/></span><div><small>Trusted device</small><strong>Ready</strong></div><span className="pfLiveDot"/></div>
     <div className="pfStatusCard"><span className="pfStatusIcon"><Icon name="shieldCheck"/></span><div><small>Zero Trust Security</small><strong>Always verifying</strong></div></div>
    </div>
    <div className="pfFutureCopy"><span>SAME PEOPLE.</span><span>SMARTER SYSTEMS.</span><span>BRIGHTER TOMORROW.</span><i/></div>
   </aside>
  </section>

  <div className="pfFooter">
   <div>BUILT FOR WHAT&apos;S NEXT.</div>
   <a className="pfDevBrand" href="https://perfectsolutioninternational.com/" target="_blank" rel="noopener noreferrer">
    <small>DEVELOPED BY</small>
    <Image src="/perfect-solutions-international.png" width={1287} height={397} alt="Perfect Solution International"/>
   </a>
   <div>TRUST <span>•</span> INTELLIGENCE <span>•</span> PROGRESS</div>
  </div>
 </main>;
}
