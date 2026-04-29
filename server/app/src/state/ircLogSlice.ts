import { createSlice, PayloadAction } from "@reduxjs/toolkit";

// Cap to keep memory bounded if the IRC server is chatty (e.g. the bot
// is in a busy channel). Older entries roll off the front.
const MAX_ENTRIES = 500;

export interface IrcLogEntry {
  line: string;
  timestamp: number;
}

interface IrcLogState {
  entries: IrcLogEntry[];
}

const initialState: IrcLogState = {
  entries: []
};

const ircLogSlice = createSlice({
  name: "ircLog",
  initialState,
  reducers: {
    appendEntry(state, action: PayloadAction<IrcLogEntry>) {
      state.entries.push(action.payload);
      if (state.entries.length > MAX_ENTRIES) {
        state.entries.splice(0, state.entries.length - MAX_ENTRIES);
      }
    },
    clearEntries(state) {
      state.entries = [];
    }
  }
});

export const { appendEntry, clearEntries } = ircLogSlice.actions;
export { ircLogSlice, MAX_ENTRIES };
export default ircLogSlice.reducer;
