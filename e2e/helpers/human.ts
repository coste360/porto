import type { Locator, Page, TestInfo } from "@playwright/test";

// Accounted human time of the rental phone flows (m11-plan §16.4, D25, spec 32 M11 criterion 2): every interaction
// of a scripted flow goes through Human, which counts it and adds the time a person would take (tap 1.2 s, typed
// field 1.0 s + 0.3 s per character, select 2.0 s, photo 4.0 s, signature 6.0 s, screen read 2.0 s per wizard
// step). The spec asserts measured wall time + accounted time < 180 s and the caps on taps, typed fields and photos.
// PORTO_E2E_REAL_HUMAN=1 really waits the delays (manual run).

export const HUMAN_MS = { tap: 1200, field: 1000, perChar: 300, select: 2000, photo: 4000, signature: 6000, screen: 2000 } as const;

export class Human {
  taps = 0;
  fields = 0;
  selects = 0;
  photos = 0;
  signatures = 0;
  screens = 0;
  accountedMs = 0;
  private readonly start = Date.now();
  private readonly real = process.env.PORTO_E2E_REAL_HUMAN === "1";

  constructor(private readonly page: Page) {}

  private async spend(ms: number): Promise<void> {
    this.accountedMs += ms;
    if (this.real) await this.page.waitForTimeout(ms);
  }

  async tap(target: Locator): Promise<void> {
    this.taps++;
    await this.spend(HUMAN_MS.tap);
    await target.click();
  }

  async type(target: Locator, text: string): Promise<void> {
    this.fields++;
    await this.spend(HUMAN_MS.field + HUMAN_MS.perChar * text.length);
    await target.fill(text);
  }

  async select(target: Locator, value: string): Promise<void> {
    this.selects++;
    await this.spend(HUMAN_MS.select);
    await target.fill(value);
  }

  async photo(input: Locator, png: Buffer): Promise<void> {
    this.photos++;
    await this.spend(HUMAN_MS.photo);
    await input.setInputFiles({ name: `photo-${this.photos}.png`, mimeType: "image/png", buffer: png });
  }

  async sign(canvas: Locator): Promise<void> {
    this.signatures++;
    await this.spend(HUMAN_MS.signature);
    const box = await canvas.boundingBox();
    if (!box) throw new Error("signature canvas not visible");
    const y = box.y + box.height / 2;
    await this.page.mouse.move(box.x + 20, y);
    await this.page.mouse.down();
    for (let i = 1; i <= 12; i++) await this.page.mouse.move(box.x + 20 + i * ((box.width - 40) / 12), y + (i % 2 ? -15 : 15));
    await this.page.mouse.up();
  }

  /** A wizard step read by the person. */
  async screen(): Promise<void> {
    this.screens++;
    await this.spend(HUMAN_MS.screen);
  }

  /** Measured wall time plus accounted human time (when the delays are not really waited). */
  totalMs(): number {
    const wall = Date.now() - this.start;
    return this.real ? wall : wall + this.accountedMs;
  }

  /** Prints the breakdown and attaches it to the report. */
  async report(info: TestInfo, name: string): Promise<void> {
    const body = JSON.stringify(
      {
        flow: name,
        taps: this.taps,
        typedFields: this.fields,
        selects: this.selects,
        photos: this.photos,
        signatures: this.signatures,
        screens: this.screens,
        accountedSeconds: this.accountedMs / 1000,
        totalSeconds: this.totalMs() / 1000,
      },
      null,
      2,
    );
    console.log(body);
    await info.attach(`${name}-human-time`, { body, contentType: "application/json" });
  }
}
