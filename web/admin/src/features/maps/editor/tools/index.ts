// Editor tools (m1-plan §5.3): what each tool collects from the canvas and its keyboard shortcut.
export const TOOL_IDS = [
  'select',
  'pan',
  'quay',
  'pier',
  'walkway',
  'finger',
  'polygon',
  'berth',
  'buoy',
  'linear_area',
  'surface_area',
  'land_resource',
  'pedestal',
  'decoration',
  'text',
  'depth_label',
  'measure',
  'calibrate',
] as const;

export type ToolId = (typeof TOOL_IDS)[number];

/** How many clicks a tool collects: one, two, or many (finished with Enter or a double click). */
export type ClickMode = 'none' | 'one' | 'two' | 'many';

export type ToolDef = { id: ToolId; shortcut: string; clicks: ClickMode; minPoints: number };

export const TOOLS: Record<ToolId, ToolDef> = {
  select: { id: 'select', shortcut: 'V', clicks: 'none', minPoints: 0 },
  pan: { id: 'pan', shortcut: 'H', clicks: 'none', minPoints: 0 },
  quay: { id: 'quay', shortcut: 'Q', clicks: 'many', minPoints: 2 },
  pier: { id: 'pier', shortcut: 'P', clicks: 'two', minPoints: 2 },
  walkway: { id: 'walkway', shortcut: 'W', clicks: 'many', minPoints: 2 },
  finger: { id: 'finger', shortcut: 'F', clicks: 'one', minPoints: 1 },
  polygon: { id: 'polygon', shortcut: 'G', clicks: 'many', minPoints: 3 },
  berth: { id: 'berth', shortcut: 'B', clicks: 'one', minPoints: 1 },
  buoy: { id: 'buoy', shortcut: 'O', clicks: 'one', minPoints: 1 },
  linear_area: { id: 'linear_area', shortcut: 'L', clicks: 'two', minPoints: 2 },
  surface_area: { id: 'surface_area', shortcut: 'S', clicks: 'many', minPoints: 3 },
  land_resource: { id: 'land_resource', shortcut: 'R', clicks: 'one', minPoints: 1 },
  pedestal: { id: 'pedestal', shortcut: 'E', clicks: 'one', minPoints: 1 },
  decoration: { id: 'decoration', shortcut: 'D', clicks: 'one', minPoints: 1 },
  text: { id: 'text', shortcut: 'T', clicks: 'one', minPoints: 1 },
  depth_label: { id: 'depth_label', shortcut: 'U', clicks: 'one', minPoints: 1 },
  measure: { id: 'measure', shortcut: 'M', clicks: 'two', minPoints: 2 },
  calibrate: { id: 'calibrate', shortcut: 'C', clicks: 'two', minPoints: 2 },
};

/** Tool of a plain letter key, if any. */
export function toolForKey(key: string): ToolId | null {
  const k = key.toUpperCase();
  for (const t of TOOL_IDS) if (TOOLS[t].shortcut === k) return t;
  return null;
}

export type PolygonTarget = 'breakwater' | 'channel' | 'water' | 'land' | 'concession';
export const POLYGON_TARGETS: readonly PolygonTarget[] = [
  'breakwater',
  'channel',
  'water',
  'land',
  'concession',
];
