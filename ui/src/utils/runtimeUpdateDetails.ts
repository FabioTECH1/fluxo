import type { PHPUpdateBranch } from '../composables/usePHPUpdates';
import type { NodeUpdateStatus } from '../composables/useNodeUpdates';
export interface RuntimeChange { name: string; installed: string; candidate: string; kind: string; source: string }
export interface RuntimeSummary { id: string; branch: string; label: string; runtime: 'php' | 'node'; before: string; after: string; target: string | null; kind: string; changes: RuntimeChange[] }
export interface ReleaseHighlights { state: string; date?: string; security: boolean; highlights: string[]; url?: string; detail?: string }
export function upstreamVersion(value: string): string | null {
 return value.replace(/^\d+:/,'').match(/^(\d+\.\d+\.\d+)(?:$|[-+])/)?.[1] || null;
}
export function phpSummary(branch: PHPUpdateBranch): RuntimeSummary {
 const changes = (branch.plan?.inventory.changes || []).map(p => ({name:p.name,installed:p.installed || 'Not installed',candidate:p.candidate,kind:p.candidate_source === `php${branch.version}` || new RegExp(`^php${branch.version.replace('.','\\.')}-(fpm|cli|common|cgi|phpdbg|opcache)$`).test(p.name) ? 'PHP core / bundled extension' : 'Extension',source:[p.candidate_source,...new Set((p.origins || []).map(o=>o.site || o.origin).filter(Boolean))].filter(Boolean).join(' · ') || 'Source unavailable'}));
 const core = changes.filter(p => p.kind !== 'Extension');
 const unique = (values: (string|null)[]) => { const v=[...new Set(values)]; return v.length===1 ? v[0] || null : null; };
 const before = unique(core.map(p=>upstreamVersion(p.installed))), after = unique(core.map(p=>upstreamVersion(p.candidate)));
 const kind = !changes.length ? 'Package details unavailable' : !core.length ? 'Extension updates' : before && before === after ? 'Package revision update' : 'PHP release update';
 return {id:branch.plan?.id || '',branch:branch.version,label:`PHP ${branch.version}`,runtime:'php',before:before || 'See package changes',after:after || 'See package changes',target:kind==='PHP release update'?after:null,kind,changes};
}
export function nodeSummary(snapshot: NodeUpdateStatus['snapshot']): RuntimeSummary {
 const installed = snapshot.installed?.release || snapshot.legacy_source;
 const candidate = snapshot.candidate;
 return {id:snapshot.discovery_id,branch:'',label:'Node.js',runtime:'node',before:installed?.version || 'Unavailable',after:candidate?.version || 'Unavailable',target:candidate?.version && /^\d+\.\d+\.\d+$/.test(candidate.version)?candidate.version:null,kind:snapshot.migration_available?'Node.js migration':'Node.js release update',changes:[
 {name:'Node.js',installed:installed?.version || 'Unavailable',candidate:candidate?.version || 'Unavailable',kind:'Runtime',source:snapshot.legacy_source?'APT → official Node.js':'Official Node.js'},
 {name:'npm',installed:installed?.npm || 'Unavailable',candidate:candidate?.npm || 'Unavailable',kind:'Bundled component',source:'Bundled with Node.js'},
 ]};
}
export function summaryLabel(summary: RuntimeSummary): string {
 if(!summary.changes.length)return `${summary.label} update available`;
 if (summary.runtime==='node') return `Node.js ${summary.after} update available`;
 return summary.target ? `PHP ${summary.target} update available` : `${summary.label}: ${summary.kind.toLowerCase()} available`;
}
