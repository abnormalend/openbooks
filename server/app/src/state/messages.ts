export enum NotificationType {
  NOTIFY,
  SUCCESS,
  WARNING,
  DANGER
}

export enum MessageType {
  STATUS,
  CONNECT,
  SEARCH,
  DOWNLOAD,
  RATELIMIT,
  IRC_MESSAGE
}

// Notification is used to show a UI toast notification the the user.
export interface Notification {
  appearance: NotificationType;
  title: string;
  detail?: string;
  timestamp: number;
}

// Response is received from websocket requests
export interface Response extends Omit<Notification, "timestamp"> {
  type: MessageType;
}

// ConnectionResponse is received after successful IRC connection
export interface ConnectionResponse extends Response {
  name: string;
}

// SearchResponse is received after search results are received and parsed.
export interface SearchResponse extends Response {
  books: BookDetail[];
  errors: ParseError[];
}

// DownloadResponse is received after file is downloaded from IRC and ready for
// user download.
export interface DownloadResponse extends Response {
  downloadPath?: string;
}

export interface BookDetail {
  server: string;
  author: string;
  title: string;
  format: string;
  size: string;
  full: string;
}

export interface ParseError {
  error: string;
  line: string;
}

// IrcLogResponse carries a single raw IRC line for the log panel. The
// frontend appends it to ircLogSlice without raising a notification toast,
// so this shape intentionally does not extend Response - no
// appearance/title/detail fields would be useful.
export interface IrcLogResponse {
  type: MessageType;
  line: string;
  timestamp: number;
}
