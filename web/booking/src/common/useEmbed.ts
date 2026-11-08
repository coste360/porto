import { createContext, useContext } from 'react';

import { createMessenger, type Messenger } from './embed';

export type EmbedState = {
  /** The page runs inside an iframe opened through the widget (?embed=1). */
  embedded: boolean;
  /** Posts to the verified parent only (a no-op otherwise). */
  messenger: Messenger;
  /** The embedding page, sent as return_to of the checkout (kept by the server only for allowed origins). */
  returnTo: string | null;
};

export const embedOff: EmbedState = {
  embedded: false,
  messenger: createMessenger(null, null),
  returnTo: null,
};

export const EmbedContext = createContext<EmbedState>(embedOff);

export function useEmbed(): EmbedState {
  return useContext(EmbedContext);
}
