import chalk from "chalk";
import {
  Container,
  Editor,
  matchesKey,
  ProcessTerminal,
  ScrollView,
  TuiAltScreen,
  VStack,
  type EditorTheme,
  type TUI,
} from "@earendil-works/pi-tui";
import { PiHarness } from "../harness.js";
import type { ApprovalRequest, HarnessEvent, HarnessMode, SessionSnapshot } from "../types.js";
import { ApprovalView, FooterView, HeaderView, TranscriptView } from "./components.js";

const editorTheme: EditorTheme = {
  borderColor: (text) => chalk.hex("#454b61")(text),
  selectList: {
    selectedPrefix: (text) => chalk.hex("#8aa9ff")(text),
    selectedText: (text) => chalk.hex("#8aa9ff").bold(text),
    description: (text) => chalk.hex("#777d8f")(text),
    scrollInfo: (text) => chalk.hex("#666b7a")(text),
    noMatch: (text) => chalk.hex("#f7768e")(text),
  },
};

export type TerminalAppOptions = {
  cwd: string;
  mode: HarnessMode;
  continueRecent: boolean;
};

export class TerminalApp {
  private readonly harness: PiHarness;
  private readonly tui: TUI;
  private readonly editor: Editor;
  private readonly header: HeaderView;
  private readonly transcript: TranscriptView;
  private readonly approvalView = new ApprovalView();
  private readonly footer: FooterView;
  private approval?: ApprovalRequest;
  private unsubscribe?: () => void;
  private closing = false;
  private noticeTimer?: ReturnType<typeof setTimeout>;

  constructor(private readonly options: TerminalAppOptions) {
    this.harness = new PiHarness(options.cwd, options.mode);
    const snapshot = this.harness.getSnapshot();
    this.tui = new TuiAltScreen(new ProcessTerminal(), true, undefined, {
      mouse: true,
      searchMatchStyle: (text) => chalk.bgHex("#33405f")(text),
      searchCurrentMatchStyle: (text) => chalk.bgHex("#5c3d67").bold(text),
    });
    this.header = new HeaderView(snapshot);
    this.transcript = new TranscriptView(snapshot);
    this.footer = new FooterView(snapshot);
    this.editor = new Editor(this.tui, editorTheme, { paddingX: 1, autocompleteMaxVisible: 6 });
    this.editor.onSubmit = (text) => void this.handleSubmit(text);
    this.buildLayout();
    this.installKeybindings();
  }

  async run(): Promise<void> {
    this.unsubscribe = this.harness.subscribe((event) => this.onHarnessEvent(event));
    this.tui.setFocus(this.editor);
    this.tui.start();
    await this.harness.initialize(this.options.continueRecent);
  }

  private buildLayout(): void {
    const transcriptDocument = new Container();
    transcriptDocument.addChild(this.header);
    transcriptDocument.addChild(this.transcript);

    const transcriptScroll = new ScrollView(transcriptDocument, {
      follow: "end",
      primary: true,
      overscroll: "contain",
      scrollbar: "auto",
      scrollbarStyle: (text) => chalk.hex("#414862")(text),
    });
    const composer = new VStack([this.approvalView, this.editor, this.footer]);
    const layout = new VStack([
      { component: transcriptScroll, basis: 0, grow: 1, minSize: 4 },
      { component: composer, basis: "auto", shrink: 1, minSize: 3 },
    ]);
    if (this.tui instanceof TuiAltScreen) this.tui.setLayoutRoot(layout);
  }

  private installKeybindings(): void {
    this.tui.addInputListener((data) => {
      if (this.approval) {
        if (matchesKey(data, "y")) {
          this.answerApproval(true, false);
          return { consume: true };
        }
        if (matchesKey(data, "a")) {
          this.answerApproval(true, true);
          return { consume: true };
        }
        if (matchesKey(data, "n") || matchesKey(data, "escape")) {
          this.answerApproval(false, false);
          return { consume: true };
        }
        return { consume: true };
      }

      if (matchesKey(data, "ctrl+q")) {
        void this.close();
        return { consume: true };
      }
      if (matchesKey(data, "ctrl+c")) {
        if (this.harness.getSnapshot().streaming) void this.harness.abort();
        else void this.close();
        return { consume: true };
      }
      if (matchesKey(data, "shift+tab")) {
        this.harness.cycleMode();
        return { consume: true };
      }
      if (matchesKey(data, "ctrl+t")) {
        this.runCommand(() => this.harness.cycleThinking());
        return { consume: true };
      }
      if (matchesKey(data, "ctrl+k")) {
        this.runCommand(() => this.harness.compact());
        return { consume: true };
      }
      if (matchesKey(data, "ctrl+n")) {
        this.runCommand(() => this.harness.newSession());
        return { consume: true };
      }
      return undefined;
    });
  }

  private async handleSubmit(input: string): Promise<void> {
    const text = input.trim();
    if (!text) return;
    this.editor.addToHistory(text);

    if (text.startsWith("/")) {
      await this.handleSlashCommand(text);
      return;
    }
    await this.harness.prompt(text);
  }

  private async handleSlashCommand(input: string): Promise<void> {
    const [command = "", argument = ""] = input.slice(1).split(/\s+/, 2);
    try {
      switch (command) {
        case "help":
          this.showNotice("/mode review|plan|auto|full · /model next|provider/id · /thinking LEVEL · /new · /compact · /quit");
          break;
        case "mode":
          if (!["review", "plan", "auto", "full"].includes(argument)) throw new Error("Use /mode review|plan|auto|full");
          this.harness.setMode(argument as HarnessMode);
          break;
        case "model":
          await this.harness.selectModel(argument || "next");
          break;
        case "models": {
          const models = this.harness.getSnapshot().models;
          this.showNotice(models.length ? models.map((model) => `${model.provider}/${model.id}`).join(" · ") : "No authenticated models.");
          break;
        }
        case "thinking":
          if (argument) this.harness.setThinking(argument);
          else this.harness.cycleThinking();
          break;
        case "new":
          await this.harness.newSession();
          break;
        case "compact":
          await this.harness.compact();
          break;
        case "quit":
        case "exit":
          await this.close();
          break;
        default:
          throw new Error(`Unknown command: /${command}. Use /help.`);
      }
    } catch (error) {
      this.showNotice(error instanceof Error ? error.message : String(error), "error");
    }
  }

  private onHarnessEvent(event: HarnessEvent): void {
    if (event.type === "snapshot") this.updateSnapshot(event.snapshot);
    if (event.type === "approval") {
      this.approval = event.request;
      this.approvalView.setRequest(event.request);
    }
    if (event.type === "notice") this.showNotice(event.message, event.level);
    this.tui.requestRender();
  }

  private updateSnapshot(snapshot: SessionSnapshot): void {
    this.header.setSnapshot(snapshot);
    this.transcript.setSnapshot(snapshot);
    this.footer.setSnapshot(snapshot);
  }

  private answerApproval(allow: boolean, alwaysAllow: boolean): void {
    if (!this.approval) return;
    this.harness.resolveApproval(this.approval.id, allow, alwaysAllow);
    this.showNotice(allow ? (alwaysAllow ? `Always allowing ${this.approval.toolName} this session.` : "Allowed once.") : "Rejected.", allow ? "success" : "error");
    this.approval = undefined;
    this.approvalView.setRequest(undefined);
    this.tui.requestRender();
  }

  private showNotice(message: string, level: "info" | "error" | "success" = "info"): void {
    if (this.noticeTimer) clearTimeout(this.noticeTimer);
    this.footer.setNotice({ level, message });
    this.tui.requestRender();
    this.noticeTimer = setTimeout(() => {
      this.footer.setNotice(undefined);
      this.tui.requestRender();
    }, 5_000);
  }

  private runCommand(command: () => unknown): void {
    try {
      const result = command();
      if (result instanceof Promise) void result.catch((error: unknown) => this.showNotice(error instanceof Error ? error.message : String(error), "error"));
    } catch (error) {
      this.showNotice(error instanceof Error ? error.message : String(error), "error");
    }
  }

  private async close(): Promise<void> {
    if (this.closing) return;
    this.closing = true;
    if (this.noticeTimer) clearTimeout(this.noticeTimer);
    this.unsubscribe?.();
    await this.harness.dispose();
    this.tui.stop();
  }
}
