import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";
const load = createTypeScriptLoader();
const { focusedSegments, parseFocusedFeedback } = load("src/lib/focusedFeedback.ts");
const { requestArtifactAudio } = load("src/lib/artifactAudio.ts");
const item = { id:"f", kind:"blocker", originalFragment:"like", occurrence:2, span:{start:11,end:15}, title:"Прошлое действие", explanation:"Используйте прошедшую форму.", ruleId:"verb-forms", practiceText:"I liked it." };
test("focused spans highlight the second exact occurrence and do not guess after changes", () => {
 const segments=focusedSegments("😀 like it like",[item]);
 assert.deepEqual(segments.filter(segment=>segment.focus).map(segment=>segment.text),["like"]);
 assert.equal(focusedSegments("😀 LIKE it LIKE",[item]).some(segment=>segment.focus),false);
});
test("focused feedback rejects zero occurrence and preserves answer sequence", () => {
 assert.equal(parseFocusedFeedback({version:1,answers:[{items:[{...item,occurrence:0}]}]}),undefined);
 assert.equal(parseFocusedFeedback({version:1,answers:[{turnSequence:3,items:[item]}]}).answers[0].turnSequence,3);
});
test("three blockers can coexist with independently budgeted praise and native tip", () => {
 const items = [item,item,item,{...item,kind:"praise"},{...item,kind:"native_tip"}];
 assert.equal(parseFocusedFeedback({version:1,answers:[{items}]}).answers[0].items.length,5);
 assert.equal(parseFocusedFeedback({version:1,answers:[{items:[...items,item]}]}).answers[0].items.length,6);
 for(const extra of [{...item,kind:"praise"},{...item,kind:"native_tip"}]) {
  assert.equal(parseFocusedFeedback({version:1,answers:[{items:[...items,extra]}]}),undefined);
 }
});
test("practice uses the standalone example and ignores discontinued contextual metadata", () => {
 const parsed = parseFocusedFeedback({version:1,answers:[{items:[{...item,practiceContext:{originalText:"I go home.",correctedText:"I went home.",audioFeedbackId:"context-v1-1"}}]}]});
 assert.equal(parsed.answers[0].items[0].practiceText,item.practiceText);
 assert.equal(Object.hasOwn(parsed.answers[0].items[0],"practiceContext"),false);
});
const ready = {status:"ready",asset:{id:"audio",state:"ready"},request:{method:"GET",url:"https://media.example/audio",headers:{},expiresAt:new Date(Date.now()+60000).toISOString()}};
const run = async (states, options = {}) => {
 const methods=[];let index=0;
 const ticket=await requestArtifactAudio("/audio",{...options,delay:async()=>{},request:async(_,init)=>{
  methods.push(init.method);return new Response(JSON.stringify(init.method==="GET" ? states[index++] : {status:"processing"}),{headers:{"Content-Type":"application/json"}});
 }});return {methods,ticket};
};
test("ready audio always uses GET, including reopening the same card", async()=>{
 for(let i=0;i<2;i++){const result=await run([ready]);assert.deepEqual(result.methods,["GET"]);}
});
test("first listen starts exactly once and processing polls only GET", async()=>{
 const first=await run([{status:"not_requested"},{status:"processing"},ready]);assert.deepEqual(first.methods,["GET","POST","GET","GET"]);
 const processing=await run([{status:"processing"},ready]);assert.deepEqual(processing.methods,["GET","GET"]);
});
test("failed synthesis requires an explicit retry and network failures never POST", async()=>{
 await assert.rejects(run([{status:"failed"}]),/подготовить/);
 const retry=await run([{status:"failed"},ready],{retryFailed:true});assert.deepEqual(retry.methods,["GET","POST","GET"]);
 const calls=[];await assert.rejects(requestArtifactAudio("/audio",{retryFailed:true,request:async(_,init)=>{calls.push(init.method);return new Response("",{status:503});}}));assert.deepEqual(calls,["GET"]);
});
test("expired signed URL is an error without restarting synthesis",async()=>{
 const calls=[];await assert.rejects(requestArtifactAudio("/audio",{retryFailed:true,request:async(_,init)=>{calls.push(init.method);return new Response(JSON.stringify({...ready,request:{...ready.request,expiresAt:"2000-01-01T00:00:00Z"}}));}}),/expired/);assert.deepEqual(calls,["GET"]);
});
test("all 25 anchored errors survive parsing and render as highlights", () => {
 const sentence="I go yesterday. ", text=sentence.repeat(25);
 const items=Array.from({length:25},(_,index)=>({...item,id:`error-${index}`,originalFragment:"go",correctedFragment:"went",occurrence:index+1,span:{start:index*sentence.length+2,end:index*sentence.length+4}}));
 const feedback=parseFocusedFeedback({version:1,answers:[{items}]});
 assert.equal(feedback.answers[0].items.length,25);
 assert.equal(focusedSegments(text,feedback.answers[0].items).filter(segment=>segment.focus).length,25);
});
