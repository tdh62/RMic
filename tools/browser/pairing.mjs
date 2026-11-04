import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const root=fileURLToPath(new URL('../../',import.meta.url));
const config=await mkdtemp(`${tmpdir()}/rmic-pairing-`);
const reservation=createServer();await new Promise(resolve=>reservation.listen(0,'127.0.0.1',resolve));
const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
const url=`https://127.0.0.1:${port}/`;
const errors=[];
let app,browser,output='';
async function until(check,description){
 for(let i=0;i<150;i++){if(await check())return;await new Promise(resolve=>setTimeout(resolve,100));}
 throw new Error(`Timed out: ${description}\n${output.slice(-8000)}`);
}
async function start(){
 output='';
 app=spawn('script',['-qfec','stty rows 45 cols 140; exec "$RMIC_PAIR_BINARY" --mode=duplex --ip=127.0.0.1 --listen=127.0.0.1:"$RMIC_PAIR_PORT" --udp-port=0 --config-dir="$RMIC_PAIR_CONFIG"','/dev/null'],{cwd:root,env:{...process.env,TERM:'xterm-256color',RMIC_PAIR_BINARY:`${root}bin/rmic`,RMIC_PAIR_PORT:String(port),RMIC_PAIR_CONFIG:config}});
 app.stdin.on('error',()=>{});
 app.stdout.on('data',chunk=>{output+=chunk;if(chunk.includes(Buffer.from('\x1b[6n')))app.stdin.write('\x1b[1;1R');});
 app.stderr.on('data',chunk=>{output+=chunk;});
 await until(()=>output.includes('等待设备'),'interactive startup');
}
async function quit(){
 if(!app||app.exitCode!==null)return;
 app.stdin.write('\x1b');await new Promise(resolve=>setTimeout(resolve,100));app.stdin.write('q');
 await until(()=>app.exitCode!==null,'clean TUI shutdown');assert.equal(app.exitCode,0);
}
try{
 await start();
 browser=await chromium.launch({headless:true,args:['--use-fake-device-for-media-stream','--use-fake-ui-for-media-stream']});
 const context=await browser.newContext({ignoreHTTPSErrors:true,permissions:['microphone'],viewport:{width:390,height:844}});
 let page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(url);
 await page.locator('#pairing').waitFor({state:'visible'});
 await page.locator('#device-name').fill('历史授权测试手机');await page.locator('#request-pair').click();
 await page.locator('#pair-code').waitFor({state:'visible'});const code=await page.locator('#pair-code').textContent();assert.match(code,/^\d{6}$/);
 assert.equal(await page.evaluate(async()=> (await fetch('/api/status')).status),401);
 app.stdin.write('p');await until(()=>output.includes(code)&&output.includes('历史授权测试手机'),'matching confirmation code on PC');
 app.stdin.write('\r');await until(()=>output.includes('已允许设备'),'local approval');
 await page.locator('#authorization').waitFor({state:'visible'});
 const authorization=await page.evaluate(()=>JSON.parse(localStorage.getItem('rmic-device-auth')));assert.equal(authorization.token.length,48);
 const state=JSON.parse(await readFile(`${config}/trusted-devices.json`,'utf8'));assert.equal(state.devices.length,1);assert.equal(JSON.stringify(state).includes(authorization.token),false);
 console.log('PASS tokenless request, matching PC confirmation, local approval and hashed PC records');
 await page.getByRole('button',{name:'开始传音'}).click();await page.locator('#status').filter({hasText:/^正在传音$/}).waitFor();
 await page.getByRole('button',{name:'断开连接',exact:true}).click();await page.close();
 page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));await page.goto(url);
 await until(()=>page.evaluate(async()=>{try{return (await api('/api/status')).state==='waiting';}catch{return false;}}),'browser history reuse');
 await quit();await start();await page.reload();
 await until(()=>page.evaluate(async()=>{try{return (await api('/api/status')).state==='waiting';}catch{return false;}}),'persisted approval after PC restart');
 assert.equal(await page.locator('#pairing').isHidden(),true);
 assert.equal(JSON.parse(await readFile(`${config}/trusted-devices.json`,'utf8')).devices.length,1);
 console.log('PASS authorization reuse after closing phone tab and restarting computer service');
 await page.getByRole('button',{name:'开始传音'}).click();await page.locator('#status').filter({hasText:/^正在传音$/}).waitFor();
 await page.locator('#audio-settings > summary').click();
 await page.getByLabel('连接方式').selectOption('ws-pcm');await page.locator('#status').filter({hasText:/^正在传音$/}).waitFor();
 await page.getByLabel('PCM 压缩').selectOption('deflate');await page.locator('#status').filter({hasText:/^正在传音$/}).waitFor();
 assert.equal(await page.evaluate(()=>current.socket.readyState),1);
 app.stdin.write('p');await until(()=>output.includes('RMic · 配对'),'history panel');app.stdin.write('\t');await until(()=>output.includes('历史授权测试手机'),'remembered device visible');app.stdin.write('r');await until(()=>output.includes('撤销当前设备'),'revoke confirmation');app.stdin.write('y');await until(()=>output.includes('已撤销该设备授权'),'revoke saved');
 await page.locator('#pairing').waitFor({state:'visible',timeout:15000});
 assert.equal(await page.evaluate(async()=> (await fetch('/api/status',{headers:{Authorization:`Bearer ${JSON.parse(sessionStorage.getItem('rmic-device-auth')||'null')?.token||''}`}})).status),401);
 assert.equal(JSON.parse(await readFile(`${config}/trusted-devices.json`,'utf8')).devices.length,0);
 assert.deepEqual(errors,[]);console.log('PASS PC revocation stops WebSocket PCM audio and removes mobile authorization');
}finally{
 await writeFile('/tmp/rmic-pairing-tui.log',output);
 await browser?.close();try{await quit();}catch{app?.kill('SIGTERM');}
 await rm(config,{recursive:true,force:true});
}
