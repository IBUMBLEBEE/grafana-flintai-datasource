// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';
import { MessageChannel as NodeMessageChannel } from 'node:worker_threads';

// React 19's browser server renderer expects this Web API, which jsdom does
// not currently install on the test global.
const testMessageChannels = new Set();

class TestMessageChannel extends NodeMessageChannel {
  constructor() {
    super();
    this.port1.unref();
    this.port2.unref();
    testMessageChannels.add(this);
  }
}

globalThis.MessageChannel ??= TestMessageChannel;

afterAll(() => {
  for (const channel of testMessageChannels) {
    channel.port1.close();
    channel.port2.close();
  }
  testMessageChannels.clear();
});

// Combobox measures text via canvas; jsdom has no real 2d context.
if (typeof HTMLCanvasElement !== 'undefined') {
  HTMLCanvasElement.prototype.getContext = () => ({
    font: '',
    measureText: () => ({ width: 100 }),
  });
}
