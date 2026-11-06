import {
  AnchorIcon,
  CircleDotIcon,
  CopyIcon,
  FlipHorizontalIcon,
  HandIcon,
  HelpCircleIcon,
  ImageIcon,
  LandPlotIcon,
  MagnetIcon,
  MinusIcon,
  MousePointer2Icon,
  MoveIcon,
  PentagonIcon,
  PlugIcon,
  Redo2Icon,
  RectangleHorizontalIcon,
  RulerIcon,
  SaveIcon,
  ShapesIcon,
  SquareDashedIcon,
  SquareIcon,
  StampIcon,
  Trash2Icon,
  TypeIcon,
  Undo2Icon,
  WandSparklesIcon,
  WavesIcon,
  type LucideIcon,
} from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { Kbd } from '@porto/shared/ui/kbd';
import { Label } from '@porto/shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@porto/shared/ui/popover';
import { Separator } from '@porto/shared/ui/separator';
import { Switch } from '@porto/shared/ui/switch';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';
import { Tooltip, TooltipContent, TooltipTrigger } from '@porto/shared/ui/tooltip';

import type { GeneratorType } from '../dialogs/generators';
import type { SnapOptions } from '../model/snap';
import type { AlignMode } from '../model/transform';
import { TOOLS, TOOL_IDS, type ToolId } from '../tools';

const TOOL_ICONS: Record<ToolId, LucideIcon> = {
  select: MousePointer2Icon,
  pan: HandIcon,
  quay: MinusIcon,
  pier: RectangleHorizontalIcon,
  walkway: MoveIcon,
  finger: SquareIcon,
  polygon: PentagonIcon,
  berth: SquareDashedIcon,
  buoy: CircleDotIcon,
  linear_area: AnchorIcon,
  surface_area: LandPlotIcon,
  land_resource: ShapesIcon,
  pedestal: PlugIcon,
  decoration: StampIcon,
  text: TypeIcon,
  depth_label: WavesIcon,
  measure: RulerIcon,
  calibrate: ImageIcon,
};

const GENERATORS: readonly GeneratorType[] = [
  'pier',
  'quay_berths',
  'linear_area',
  'buoy_field',
  'land_grid',
  'surface_area',
];
const ALIGN: readonly AlignMode[] = ['left', 'centre_x', 'right', 'top', 'centre_y', 'bottom'];

export type ToolbarProps = {
  tool: ToolId;
  onTool: (tool: ToolId) => void;
  canUndo: boolean;
  canRedo: boolean;
  onUndo: () => void;
  onRedo: () => void;
  selectionCount: number;
  onGenerator: (type: GeneratorType) => void;
  onAlign: (mode: AlignMode) => void;
  onDistribute: (axis: 'x' | 'y') => void;
  onDuplicate: () => void;
  onMirror: () => void;
  onRotate: (dd: number) => void;
  onNumericMove: () => void;
  onRenumber: () => void;
  onSelectStructure: () => void;
  onDelete: () => void;
  snap: SnapOptions;
  onSnap: (snap: SnapOptions) => void;
  dirty: boolean;
  saving: boolean;
  onSave: () => void;
};

/** Editor toolbar: tools (ToggleGroup with shortcuts), history, generators, selection actions, snapping, save. */
export function Toolbar(p: ToolbarProps) {
  const { t } = useT();
  const sel = p.selectionCount;
  return (
    <div
      className="flex flex-wrap items-center gap-2 border-b p-2"
      role="toolbar"
      aria-label={t('editor:toolbar.label')}
    >
      <ToggleGroup
        type="single"
        value={p.tool}
        onValueChange={(v) => v && p.onTool(v as ToolId)}
        variant="outline"
        size="sm"
        aria-label={t('editor:toolbar.tools')}
        className="flex-wrap"
      >
        {TOOL_IDS.map((id) => {
          const Icon = TOOL_ICONS[id];
          const label = `${t(`editor:tools.${id}`)} (${TOOLS[id].shortcut})`;
          return (
            <Tooltip key={id}>
              <TooltipTrigger asChild>
                <ToggleGroupItem value={id} aria-label={label} data-tool={id}>
                  <Icon />
                </ToggleGroupItem>
              </TooltipTrigger>
              <TooltipContent>{label}</TooltipContent>
            </Tooltip>
          );
        })}
      </ToggleGroup>
      <Separator orientation="vertical" className="h-6" />
      <Button
        variant="outline"
        size="icon"
        disabled={!p.canUndo}
        onClick={p.onUndo}
        aria-label={t('editor:toolbar.undo')}
      >
        <Undo2Icon />
      </Button>
      <Button
        variant="outline"
        size="icon"
        disabled={!p.canRedo}
        onClick={p.onRedo}
        aria-label={t('editor:toolbar.redo')}
      >
        <Redo2Icon />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm">
            <WandSparklesIcon />
            {t('editor:toolbar.generators')}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          {GENERATORS.map((g) => (
            <DropdownMenuItem key={g} onSelect={() => p.onGenerator(g)}>
              {t(`editor:generators.${g}.title`)}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" disabled={sel === 0}>
            {t('editor:toolbar.arrange')}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          {ALIGN.map((m) => (
            <DropdownMenuItem key={m} disabled={sel < 2} onSelect={() => p.onAlign(m)}>
              {t(`editor:align.${m}`)}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={sel < 3} onSelect={() => p.onDistribute('x')}>
            {t('editor:align.distribute_x')}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={sel < 3} onSelect={() => p.onDistribute('y')}>
            {t('editor:align.distribute_y')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={p.onDuplicate}>
            <CopyIcon />
            {t('editor:actions.duplicate')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={p.onMirror}>
            <FlipHorizontalIcon />
            {t('editor:actions.mirror')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => p.onRotate(900)}>
            {t('editor:actions.rotate90')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => p.onRotate(-900)}>
            {t('editor:actions.rotateMinus90')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={p.onNumericMove}>
            {t('editor:actions.numericMove')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={p.onRenumber}>
            {t('editor:actions.renumber')}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={p.onSelectStructure}>
            {t('editor:actions.selectStructure')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button
        variant="outline"
        size="icon"
        disabled={sel === 0}
        onClick={p.onDelete}
        aria-label={t('editor:actions.delete')}
      >
        <Trash2Icon />
      </Button>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="outline" size="sm">
            <MagnetIcon />
            {t('editor:snap.title')}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-64">
          <ul className="flex flex-col gap-2">
            {(['grid', 'edges', 'vertices', 'align'] as const).map((k) => (
              <li key={k} className="flex items-center justify-between gap-2">
                <Label htmlFor={`snap-${k}`}>{t(`editor:snap.${k}`)}</Label>
                <Switch
                  id={`snap-${k}`}
                  checked={p.snap[k]}
                  onCheckedChange={(on) => p.onSnap({ ...p.snap, [k]: on })}
                />
              </li>
            ))}
          </ul>
          <p className="text-muted-foreground mt-3 text-xs">{t('editor:snap.altHint')}</p>
        </PopoverContent>
      </Popover>
      <ShortcutsHelp />
      <div className="ml-auto flex items-center gap-2">
        {p.dirty ? <Badge variant="secondary">{t('editor:status.unsaved')}</Badge> : null}
        <Button onClick={p.onSave} disabled={p.saving || !p.dirty}>
          <SaveIcon />
          {p.saving ? t('common:actions.saving') : t('common:actions.save')}
        </Button>
      </div>
    </div>
  );
}

const SHORTCUTS: readonly [string, string][] = [
  ['Ctrl+Z', 'undo'],
  ['Shift+Ctrl+Z / Ctrl+Y', 'redo'],
  ['Ctrl+S', 'save'],
  ['Ctrl+A', 'selectAll'],
  ['Ctrl+D', 'duplicate'],
  ['Del', 'delete'],
  ['Esc', 'escape'],
  ['Enter', 'finish'],
  ['← ↑ → ↓', 'nudge'],
  ['[ ]', 'rotate'],
  ['Shift', 'shift'],
  ['Alt', 'alt'],
];

function ShortcutsHelp() {
  const { t } = useT();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t('editor:shortcuts.title')}>
          <HelpCircleIcon />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-80">
        <p className="mb-2 text-sm font-medium">{t('editor:shortcuts.title')}</p>
        <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
          {SHORTCUTS.map(([keys, id]) => (
            <div key={id} className="contents">
              <dt>
                <Kbd>{keys}</Kbd>
              </dt>
              <dd>{t(`editor:shortcuts.${id}`)}</dd>
            </div>
          ))}
          {TOOL_IDS.map((id) => (
            <div key={id} className="contents">
              <dt>
                <Kbd>{TOOLS[id].shortcut}</Kbd>
              </dt>
              <dd>{t(`editor:tools.${id}`)}</dd>
            </div>
          ))}
        </dl>
      </PopoverContent>
    </Popover>
  );
}
