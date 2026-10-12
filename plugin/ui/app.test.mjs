import test from 'node:test';
import assert from 'node:assert/strict';
import {remaining, quota, filterSessions, mergeTurns} from './app.mjs';

test('unknown quota is not displayed as free capacity; named bucket takes precedence',()=>{
  assert.equal(remaining(null),null);
  assert.equal(remaining({usedPercent:null}),null);
  assert.equal(remaining({usedPercent:0}),100);
  assert.equal(remaining({usedPercent:150}),0);
  const q=quota({rateLimits:{primary:{usedPercent:0}},rateLimitsByLimitId:{codex:{primary:{usedPercent:90}}}});
  assert.equal(q.primary,10);assert.equal(q.secondary,null);
});
test('task filters retain native conversations and use stable managed IDs',()=>{
  const entries=[{job_id:'a',thread_id:'thread-a',title:'First',managed:true,state:'running',cwd:'/one',account:'account-secondary'},{thread_id:'b',title:'Native',state:'saved',cwd:'/two'}];
  assert.equal(filterSessions(entries,'','all','').length,2);
  assert.equal(filterSessions(entries,'account-secondary','managed','/one')[0].job_id,'a');
  assert.equal(filterSessions(entries,'','starred','',['b'])[0].thread_id,'b');
});
test('older history survives live refresh and duplicate turns receive the latest state',()=>{
  const first=[{id:'old',items:['old']},{id:'live',items:['partial']}];
  const latest=[{id:'live',items:['done']},{id:'new',items:[]}];
  assert.deepEqual(mergeTurns(first,latest),[first[0],...latest]);
});
