'use strict';
class PCMCapture extends AudioWorkletProcessor {
  constructor() {
    super(); this.frame = new Int16Array(1920); this.position = 0;
  }
  process(inputs) {
    const input = inputs[0];
    if (!input?.[0]) return true;
    for (let i=0;i<input[0].length;i++) {
      for (let channel=0;channel<2;channel++) {
        const value=input[Math.min(channel,input.length-1)][i];
        this.frame[this.position++]=Math.max(-32768,Math.min(32767,Math.round(value*32768)));
      }
      if (this.position===1920) {
        const packet=this.frame.buffer;this.port.postMessage(packet,[packet]);
        this.frame=new Int16Array(1920);this.position=0;
      }
    }
    return true;
  }
}
registerProcessor('rmic-capture',PCMCapture);
