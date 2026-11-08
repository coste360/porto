// Calendar file of a confirmed stay (m8-plan §12.1), built in the browser: one all-day event from the arrival
// to the departure date (the departure is exclusive, as nights are the occupancy unit). No personal data.

function escapeText(s: string): string {
  return s
    .replace(/\\/g, '\\\\')
    .replace(/\n/g, '\\n')
    .replace(/([,;])/g, '\\$1');
}

const compact = (isoDate: string) => isoDate.replace(/-/g, '');

export function buildIcs(e: {
  uid: string;
  arrival: string;
  departure: string;
  summary: string;
  description?: string;
  stamp: Date;
}): string {
  const stamp = e.stamp
    .toISOString()
    .replace(/[-:]/g, '')
    .replace(/\.\d{3}/, '');
  const lines = [
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    'PRODID:-//Coste360//Porto//IT',
    'CALSCALE:GREGORIAN',
    'BEGIN:VEVENT',
    `UID:${e.uid}@porto`,
    `DTSTAMP:${stamp}`,
    `DTSTART;VALUE=DATE:${compact(e.arrival)}`,
    `DTEND;VALUE=DATE:${compact(e.departure)}`,
    `SUMMARY:${escapeText(e.summary)}`,
    ...(e.description ? [`DESCRIPTION:${escapeText(e.description)}`] : []),
    'END:VEVENT',
    'END:VCALENDAR',
  ];
  return `${lines.join('\r\n')}\r\n`;
}

export function downloadIcs(fileName: string, content: string): void {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/calendar;charset=utf-8' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = fileName;
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
