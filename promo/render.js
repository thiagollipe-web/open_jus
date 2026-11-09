// Renderiza promo/reels.html em MP4 1080x1920 30fps, quadro a quadro (sem perder quadros).
// Uso: node promo/render.js   (requer playwright e ffmpeg)
const {chromium}=require('playwright');const {spawn}=require('child_process');const path=require('path');
const FPS=30,DUR=19,OUT=path.join(__dirname,'openjus-reels.mp4');
(async()=>{
  const b=await chromium.launch();const p=await b.newPage({viewport:{width:1080,height:1920},deviceScaleFactor:1});
  await p.goto('file://'+path.join(__dirname,'reels.html')+'?render=1');await p.waitForLoadState('networkidle');
  await p.evaluate(()=>document.fonts.ready);
  const ff=spawn('ffmpeg',['-y','-f','image2pipe','-framerate',String(FPS),'-i','-','-c:v','libx264','-preset','medium',
    '-crf','18','-pix_fmt','yuv420p','-movflags','+faststart',OUT],{stdio:['pipe','inherit','inherit']});
  for(let f=0;f<FPS*DUR;f++){
    await p.evaluate(t=>document.getAnimations().forEach(a=>{a.pause();a.currentTime=t}),f*1000/FPS);
    ff.stdin.write(await p.screenshot({type:'jpeg',quality:95}));
  }
  ff.stdin.end();await new Promise(r=>ff.on('close',r));await b.close();console.log('ok',OUT);
})();
