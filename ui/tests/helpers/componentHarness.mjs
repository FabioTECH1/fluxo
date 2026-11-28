import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { parse, compileScript } from '@vue/compiler-sfc';
import * as vue from 'vue';
import * as router from 'vue-router';
let id=0;
export async function load(source,bindings={}) {
 const key=`dashboardFlow${++id}`;globalThis[key]={vue,'vue-router':router,...bindings};
 const raw=stripTypeScriptTypes(source).replace(/\bimport ([^;\n]+?) from ['"]([^'"]+)['"];?/g,(_,names,path)=>{
  if(path.includes('/utils/'))return `import ${names} from ${JSON.stringify(new URL('../../src/utils/'+path.split('/').at(-1)+'.ts',import.meta.url).href)};`;
  if(!(path in globalThis[key]))throw Error('Missing binding '+path);
  return `const ${names.startsWith('{')?names.replace(/\bas\b/g,':'):names}=globalThis.${key}[${JSON.stringify(path)}];`;
 });
 return import('data:text/javascript;base64,'+Buffer.from(raw).toString('base64'));
}
export async function component(file,bindings={}) {const {descriptor}=parse(await readFile(new URL('../../src/'+file,import.meta.url),'utf8'));return (await load(compileScript(descriptor,{id:file,inlineTemplate:true}).content,bindings)).default;}
export const box={setup:(_,{slots})=>()=>vue.h('div',slots.default?.())};
export const modal={props:['modelValue','title'],setup:(p,{slots,emit})=>()=>p.modelValue?vue.h('section',{role:'dialog','data-title':p.title,onClose:()=>emit('update:modelValue',false)},[slots.default?.(),slots.footer?.()]):null};
export function host(){
 globalThis.Document ||= class {};globalThis.ShadowRoot ||= class {};
 const node=(type,text='')=>({type,text,children:[],props:{},parent:null,selectedIndex:0,get options(){return this.children.filter(n=>n.type==='option')},value:'',getRootNode:()=>({}),addEventListener(){},removeEventListener(){},focus(){},getBoundingClientRect(){return {left:30,top:80,bottom:110,width:300,height:30}},contains(target){return all(this).includes(target)},scrollIntoView(){}});
 const all=n=>[n,...n.children.flatMap(all)];const text=n=>(n.type==='#comment'?'':n.text)+n.children.map(text).join('');
 const renderer=vue.createRenderer({querySelector:()=>root,createElement:t=>node(t),createText:t=>node('#text',t),createComment:t=>node('#comment',t),setText:(n,t)=>n.text=t,setElementText:(n,t)=>{n.text=t;n.children=[]},parentNode:n=>n.parent,nextSibling:n=>n.parent?.children[n.parent.children.indexOf(n)+1]||null,patchProp:(n,k,_,v)=>{n.props[k]=v;if(k==='value')n.value=v},insert(n,p,a=null){if(n.parent)n.parent.children.splice(n.parent.children.indexOf(n),1);n.parent=p;const i=a?p.children.indexOf(a):-1;p.children.splice(i<0?p.children.length:i,0,n)},remove(n){n.parent?.children.splice(n.parent.children.indexOf(n),1);n.parent=null}});
 const root=node('root');return {renderer,root,all:()=>all(root),text:()=>text(root),find:fn=>all(root).find(fn),button:label=>all(root).find(n=>n.type==='button'&&text(n).trim()===label)};
}
export async function settle(){await new Promise(r=>setImmediate(r));await vue.nextTick();}
