# Forensic parity matrix (critical domains)

| Domain | Nest | Go | Web needs | Status |
|--------|------|-----|-----------|--------|
| Auth | POST /auth/login,register | same | yes | matched static |
| Inventory | GET /inventory,summary,alerts | same + live days | yes | matched after fix |
| Received | POST /received | same dual JSON | yes | matched static |
| POS | POST /pos/order, send-kitchen, payment | server authority | yes | matched intentional |
| Kitchen | active-orders, status, closing | present | yes | matched static |
| Production | start/finish/history | dual production_id | yes | matched after fix |
| Menu/Combos | list, categories, preview | present | yes | partial depth |
| Payments | mpesa, stripe, callbacks | shop bound | yes | matched after fix |
| Waste | record, analytics | dual JSON | yes | matched static |
| Dashboard | summary,kpis,alerts | present | yes | numeric UNVERIFIED |
| Trace | GET /trace | present | yes | limit residual |
| Users | me, list, role | present | yes | matched static |
| Realtime | Nest gateways | GET /ws | yes | UNVERIFIED |
