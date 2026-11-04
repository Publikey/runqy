import{g as K,o as Q,p as z,a as B,b as O,c as X}from"./lkDo0msz.js";import{T as G,U as R,x as d,y as j,B as C,aY as $,aZ as ee,G as te,af as se,ah as F,F as W,z as ie,ak as oe,ay as ne,p as re,f as ae,g as a,d as le,j as ce,r as de,b as he,u as b,a_ as ue,av as I,h as fe,t as ge}from"./CUnfEMDr.js";import{B as q}from"./C4bj5sxi.js";import{k as M,e as ke,i as we,s as be,a as D}from"./DDXws60w.js";import{p as l,r as ve}from"./CtQkik0U.js";function _e(t,e,...i){var s=new q(t);G(()=>{const n=e()??null;s.ensure(n,n&&(_=>n(_,...i)))},R)}function Se(t,e,i,s,n,_){let S=d;d&&j();var o=null;d&&C.nodeType===$&&(o=C,j());var f=d?C:t,g=new q(f,!1);G(()=>{const c=e()||null;var m=ee;if(c===null){g.ensure(null,null),z(!0);return}return g.ensure(c,h=>{if(c){if(o=d?o:document.createElementNS(m,c),K(o,o),s){d&&Q(c)&&o.append(document.createComment(""));var u=d?te(o):o.appendChild(se());d&&(u===null?F(!1):W(u)),s(o,u)}ie.nodes.end=o,h.before(o)}d&&W(h)}),z(!0),()=>{c&&z(!1)}},R),oe(()=>{z(!0)}),S&&(F(!0),W(f))}/**
 * @file
 * @license @lucide/svelte v1.68.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const v={xmlns:"http://www.w3.org/2000/svg",width:24,height:24,viewBox:"0 0 24 24",fill:"none",stroke:"currentColor","stroke-width":2,"stroke-linecap":"round","stroke-linejoin":"round"};/**
 * @file
 * @license @lucide/svelte v1.68.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const E=(...t)=>t.filter((e,i,s)=>!!e&&e.trim()!==""&&s.indexOf(e)===i).join(" ").trim();/**
 * @file
 * @license @lucide/svelte v1.68.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */function A(t){return t!=null}function me(t,e={}){var h,u;const i=e.attributeNames??{},s=r=>i[r]??r,n=t.size??t.width??v.width,_=t.size??t.height??v.height,S=((h=t.aliases)==null?void 0:h.filter(r=>typeof r=="string"&&r.trim()!=="").map(r=>`lucide-${r}`))??[],o=[...t.name?[`lucide-${t.name}`]:[],...S],f=((u=e.className)==null?void 0:u.split(" ").filter(Boolean))??[],g=e.includeDefaultClasses===!1?E(...f):E("lucide",...o,...f),c=e.absoluteStrokeWidth?Number(e.strokeWidth??v["stroke-width"])*Number(t.size??t.width??v.width)/Number(e.size??e.width??v.width):e.strokeWidth??v["stroke-width"];return["svg",{...Object.entries(v).reduce((r,[k,w])=>(r[s(k)]=w,r),{}),..."color"in e&&e.color&&{[s("stroke")]:e.color},..."size"in e&&A(e.size)&&{[s("width")]:e.size,[s("height")]:e.size},..."width"in e&&A(e.width)&&{[s("width")]:e.width},..."height"in e&&A(e.height)&&{[s("height")]:e.height},[s("stroke-width")]:c,...g&&{[s("class")]:g},[s("viewBox")]:`0 0 ${n} ${_}`,...e.hasA11yProp===!1?{[s("aria-hidden")]:"true"}:{},..."attributes"in e&&e.attributes},t.node.map(r=>{const[k,w,N]=r,x=e.nonScalingStroke?{[s("vector-effect")]:"non-scaling-stroke",...w}:w;return N?[k,x,N]:[k,x]})]}/**
 * @file
 * @license @lucide/svelte v1.68.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const Ne=t=>{for(const e in t)if(e.startsWith("aria-")||e==="role"||e==="title")return!0;return!1};/**
 * @file
 * @license @lucide/svelte v1.68.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const xe=Symbol("lucide-context"),ye=()=>ne(xe);var ze=O("<svg><!><!></svg>");function Le(t,e){re(e,!0);const i=ye()??{},s=l(e,"color",19,()=>i.color??"currentColor"),n=l(e,"size",19,()=>i.size??24),_=l(e,"width",19,n),S=l(e,"height",19,n),o=l(e,"strokeWidth",19,()=>i.strokeWidth??2),f=l(e,"absoluteStrokeWidth",19,()=>i.absoluteStrokeWidth??!1),g=l(e,"nonScalingStroke",19,()=>i.nonScalingStroke??!1),c=l(e,"iconNode",19,()=>[]),m=l(e,"icon",19,()=>({node:c(),aliases:[],size:24})),h=ve(e,["$$slots","$$events","$$legacy","color","size","width","height","strokeWidth","absoluteStrokeWidth","nonScalingStroke","iconNode","icon","class","children"]),u=b(()=>!!e.children||Ne(h)),r=b(()=>me(m(),{color:s(),width:_(),height:S(),strokeWidth:o(),absoluteStrokeWidth:f(),nonScalingStroke:g(),className:E("lucide-icon",i.class),hasA11yProp:a(u),attributes:h})),k=b(()=>I(a(r),3)),w=b(()=>a(k)[1]),N=b(()=>ue(a(k)[2],()=>[],!0)),x=b(()=>({...a(w),class:[...a(w).class.split(" "),e.class]}));var y=ze();M(y,()=>({...a(x)}));var P=ae(y);ke(P,17,()=>a(N),we,(U,V)=>{var T=b(()=>I(a(V),2));let Y=()=>a(T)[0],Z=()=>a(T)[1];var L=X(),p=fe(L);Se(p,Y,!0,(J,We)=>{M(J,()=>({...Z()}))}),B(U,L)});var H=le(P);_e(H,()=>e.children??ce),de(y),B(t,y),he()}var Ce=O('<svg viewBox="0 0 24 24" fill="none" role="status" aria-label="Loading"><circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity="0.2" stroke-width="2.5"></circle><path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"></path></svg>');function je(t,e){let i=l(e,"size",3,16),s=l(e,"class",3,"");var n=Ce();ge(()=>{be(n,0,`rq-spin shrink-0 ${s()??""}`),D(n,"width",i()),D(n,"height",i())}),B(t,n)}export{Le as I,je as S,_e as s};
