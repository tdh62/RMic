'use strict';

// PCM stays in a bounded queue on the audio thread. Fractional reads handle
// both the output sample rate and small differences between hardware clocks.
class PCMPlayer extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const playMS = options.processorOptions.playMS;
    this.auto = playMS === 0;
    this.targetMS = this.auto ? 120 : playMS;
    this.capacity = 48000 * 3;
    this.channels = options.processorOptions.channels || 2;
    this.outputChannels = options.processorOptions.outputChannels || this.channels;
    this.planes = Array.from({length:this.outputChannels},()=>new Float32Array(this.capacity));
    this.head = 0; this.size = 0; this.phase = 0;
    this.primed = false; this.underruns = 0; this.elapsed = 0; this.stable = 0;
    this.correction = 0;
    this.port.onmessage = event => {
      const view = new DataView(event.data);
      const frames = (view.byteLength - 16) / (this.channels*2);
      if (this.size + frames > this.capacity) {
        this.port.postMessage({ type: 'error', message: '手机音频队列已满，请重新连接。' });
        return;
      }
      for (let i = 0; i < frames; i++) {
        const position = (this.head + this.size + i) % this.capacity;
        const value = channel => view.getInt16(16+(i*this.channels+channel)*2,true)/32768;
        if (this.outputChannels===2 && this.channels>2) {
          let left=value(0)+0.7071*value(2),right=value(1)+0.7071*value(2);
          left+=0.7071*value(4);right+=0.7071*value(5);
          if(this.channels===8) {left+=0.7071*value(6);right+=0.7071*value(7);}
          const divisor=this.channels===8?3.1213:2.4142;
          this.planes[0][position]=left/divisor;this.planes[1][position]=right/divisor;
        } else {
          for(let channel=0;channel<this.outputChannels;channel++) this.planes[channel][position]=value(channel);
        }
      }
      this.size += frames;
    };
  }
  process(inputs, outputs) {
    const output = outputs[0];
    const duration = output[0].length / sampleRate;
    this.elapsed += duration;
    if (!this.primed && this.size >= this.targetMS * 48) this.primed = true;
    if (this.primed) {
      this.stable += duration;
      // Packet arrivals vary by one frame. Keep normal clock correction small,
      // but recover a large backlog faster without dropping musical content.
      const excess = (this.size - this.targetMS * 48) / 48000;
      const desired = Math.max(-0.003, Math.min(0.01, excess * (excess > 0.08 ? 0.05 : 0.02)));
      const slew = duration * 0.002;
      this.correction += Math.max(-slew, Math.min(slew, desired - this.correction));
      const step = 48000 / sampleRate * (1 + this.correction);
      for (let i = 0; i < output[0].length; i++) {
        if (this.size < Math.ceil(step) + 1) {
          this.primed = false; this.phase = 0; this.underruns++; this.stable = 0;
          this.correction = 0;
          if (this.auto) this.targetMS = Math.min(2000, this.targetMS + 40);
          break;
        }
        const next = (this.head + 1) % this.capacity;
        for(let channel=0;channel<output.length;channel++) {
          output[channel][i]=this.planes[channel][this.head]*(1-this.phase)+this.planes[channel][next]*this.phase;
        }
        this.phase += step;
        const consumed = Math.floor(this.phase);
        this.phase -= consumed;
        this.head = (this.head + consumed) % this.capacity;
        this.size -= consumed;
      }
    }
    if (this.auto && this.primed && this.stable >= 15) {
      this.targetMS = Math.max(60, this.targetMS - 20); this.stable = 0;
    }
    if (this.elapsed >= 0.2) {
      this.elapsed = 0;
      this.port.postMessage({ type: 'metrics', queued_ms: this.size / 48, target_ms: this.targetMS, underruns: this.underruns });
    }
    return true;
  }
}
registerProcessor('rmic-pcm', PCMPlayer);
