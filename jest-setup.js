// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// Combobox measures its option labels; the scaffolding stub returns no context at all
HTMLCanvasElement.prototype.getContext = () => ({ measureText: () => ({ width: 0 }) });
