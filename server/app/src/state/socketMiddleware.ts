import {
  AnyAction,
  Dispatch,
  Middleware,
  MiddlewareAPI,
  PayloadAction
} from "@reduxjs/toolkit";
import { openbooksApi } from "./api";
import { deleteHistoryItem } from "./historySlice";
import { appendEntry } from "./ircLogSlice";
import {
  ConnectionResponse,
  DownloadResponse,
  IrcLogResponse,
  MessageType,
  Notification,
  NotificationType,
  Response,
  SearchResponse
} from "./messages";
import { addNotification } from "./notificationSlice";
import {
  removeInFlightDownload,
  sendMessage,
  setConnectionState,
  setSearchResults,
  setUsername
} from "./stateSlice";
import { AppDispatch, RootState } from "./store";
import { displayNotification, downloadFile } from "./util";

// Web socket redux middleware.
// Listens to socket and dispatches handlers.
// Handles send_message actions by sending to socket.
export const websocketConn =
  (wsUrl: string): Middleware =>
  ({ dispatch, getState }: MiddlewareAPI<AppDispatch, RootState>) => {
    const socket = new WebSocket(wsUrl);

    socket.onopen = () => onOpen(dispatch);
    socket.onclose = () => onClose(dispatch);
    socket.onmessage = (message) => route(dispatch, message);
    socket.onerror = (event) =>
      displayNotification({
        appearance: NotificationType.DANGER,
        title: "Unable to connect to server.",
        timestamp: new Date().getTime()
      });

    return (next: Dispatch<AnyAction>) => (action: PayloadAction<any>) => {
      // Send Message action? Send data to the socket.
      if (sendMessage.match(action)) {
        if (socket.readyState === socket.OPEN) {
          socket.send(action.payload.message);
        } else {
          displayNotification({
            appearance: NotificationType.WARNING,
            title: "Server connection closed. Reload page.",
            timestamp: new Date().getTime()
          });
        }
      }

      return next(action);
    };
  };

const onOpen = (dispatch: AppDispatch): void => {
  console.log("WebSocket connected.");
  dispatch(setConnectionState(true));
  dispatch(sendMessage({ type: MessageType.CONNECT, payload: {} }));
};

const onClose = (dispatch: AppDispatch): void => {
  console.log("WebSocket closed.");
  dispatch(setConnectionState(false));
};

const route = (dispatch: AppDispatch, msg: MessageEvent<any>): void => {
  const raw = JSON.parse(msg.data) as Response | IrcLogResponse;

  // IRC log lines feed the log panel only - no toast, no notification list.
  if (raw.type === MessageType.IRC_MESSAGE) {
    const log = raw as IrcLogResponse;
    dispatch(appendEntry({ line: log.line, timestamp: log.timestamp }));
    return;
  }

  const response = raw as Response;
  const timestamp = new Date().getTime();
  const notification: Notification = {
    ...response,
    timestamp
  };

  let notif: Notification = notification;
  switch (response.type) {
    case MessageType.STATUS:
      break;
    case MessageType.CONNECT:
      dispatch(setUsername((response as ConnectionResponse).name));
      break;
    case MessageType.SEARCH:
      dispatch(setSearchResults(response as SearchResponse));
      break;
    case MessageType.DOWNLOAD:
      downloadFile((response as DownloadResponse)?.downloadPath);
      dispatch(openbooksApi.util.invalidateTags(["books"]));
      dispatch(removeInFlightDownload());
      break;
    case MessageType.RATELIMIT:
      dispatch(deleteHistoryItem());
      break;
    default:
      console.error(response);
      notif = {
        appearance: NotificationType.DANGER,
        title: "Unknown message type. See console.",
        timestamp
      };
  }

  dispatch(addNotification(notif));
  displayNotification(notif);
};
