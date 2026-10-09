# MOBILE_CONTRACT_AUDIT.md

Mobile uses a **single** `EXPO_PUBLIC_API_URL` — switch to Go only when all paths below pass.

| Mobile constant | Path | Go | Notes |
|-----------------|------|----|-------|
| LOGIN | POST /auth/login | Y | |
| REGISTER | POST /auth/register | Y | |
| RECEIVE_GOODS | POST /received | Y | |
| GET_STOCK | GET /inventory | Y | |
| KITCHEN_CLOSING_STOCK | GET/POST /kitchen/closing-stock | Y | |
| KITCHEN_ACTIVE_ORDERS | GET /kitchen/active-orders | Y | |
| CREATE_ORDER | POST /pos/orders | Y | alias of /pos/order |
| LOG_WASTE | POST /waste | Y | alias of /waste/record |
| GET_NOTIFICATIONS | GET /notifications | Y | verify list shape |
| UPDATE_NOTIFICATION_SETTINGS | PATCH/POST settings | PARTIAL | confirm route |

## Do not switch mobile to Go until
1. Login + stock list against shared DB
2. Receive + waste + POS order
3. Closing stock submit
4. Offline queue idempotency verified

## Status
**NOT READY for mobile cutover** without live device smoke on staging.
