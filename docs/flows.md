# Order flows

## Order attributes

- `channel`: `whatsapp` or `balcao` (in-house).
- `consume`: `entrega` (delivery), `local` (eat in) or `retirada` (the client picks it up at the counter; WhatsApp only, balcão is always `local`).
- `status`: see the state machine below.
- payment: zero or more parts, each with a method (`pix`, `credito`, `debito`, `dinheiro`) and an amount. Unpaid with no parts, paid when the parts add up to the total.

## State machine

```
aberto -> cozinha -> pronto -> saiu -> fechado
   \         \          \         \      ^
    \         \          +---------\-----+   (consume = local or retirada)
     +---------+----------+---------+-> cancelado

aberto -> pronto                (Marcar como pronto: no kitchen ticket)

fechado, cancelado -> aberto   (reopen)
cozinha, pronto    -> aberto   (an item is added, removed or changed)
```

| Status      | Meaning                                  |
|-------------|------------------------------------------|
| `aberto`    | Being created or edited                  |
| `cozinha`   | Sent to the kitchen (ticket printed)     |
| `pronto`    | Food is ready                            |
| `saiu`      | Courier left (delivery only)             |
| `fechado`   | Finished                                 |
| `cancelado` | Cancelled                                |

### Rules

- Payment is independent of status. Parts can be added or removed at any status except `fechado` and `cancelado`.
- The payment sheet starts with three **Como pagar** buttons, each showing its own screen: **Tudo** pays all that is left (the amount is locked); **Dividir** shows **Dividir entre** (number of people) and fills one equal share, which can also be typed over; **Por Consumo** shows a stepper per order line where each person picks the units they had, and the part is their sum (locked). A shared item (a 1L soda for three) is divided with **Dividir item entre** (2 to 20 people) under its line: the stepper then counts parts, each costing the item price divided by the parts (rounded up, so all parts add up to the item). The division locks once a part of it is paid. The sheet opens on the mode used so far: Por consumo once a part was paid that way, Dividir while the stored split is above 1, Tudo otherwise. Each part has its own method. A part can't be more than what is left.
- Por consumo parts remember their units (`order_payment_items`): the next person can only pick units not yet paid, and the part lists them (`2x Hot roll · 1x Coca`). Removing the part frees its units. Removing an item line drops its rows but keeps the part's amount.
- A delivery with something left to pay can record a **Forma prevista** in its own **Pagamento na entrega** sheet, opened with **Paga na entrega** next to **Já pagou** in the order panel: how the client will pay, and for Dinheiro an optional **Troco para** (the note they pay with, at least what is left). It never counts as paid. The ticket prints it under A PAGAR as `Pagar com`, `Troco para` and `Levar troco` (the change the courier takes). An order eaten in or picked up pays at the counter, so it has none; switching a delivery to local or retirada clears it.
- **Marcar como pronto** (next to **Imprimir ticket**, under **Enviar à cozinha**) moves an `aberto` order with items straight to `pronto` without printing the kitchen ticket, for food made without one.
- A `fechado` or `cancelado` order can be reopened at any time. It goes back to `aberto`, keeps its payment and leaves the daily report until it is closed again.
- An order can't be closed until its parts add up to the total exactly (none missing, none over after items were removed).
- `saiu` exists only when `consume = entrega`. A `retirada` order goes from `pronto` to `fechado` when the client picks it up.
- Items can be edited while the status is `aberto`, `cozinha` or `pronto`. Any item change (add, remove, quantity, note) in `cozinha` or `pronto` moves the order back to `aberto`; tap **Enviar à cozinha** again to print the full updated ticket. Reprinting the ticket is allowed at any status.

## Flow 1: WhatsApp delivery

1. Open the app and tap **Novo pedido**.
2. Choose WhatsApp. Enter the client name and, optionally, the last 4 digits of the phone.
3. Add items: Adicionar item, then category, item, variant and quantity.
4. Tap **Copiar confirmação** and paste it into WhatsApp.
5. If the client paid with PIX, mark the order as paid with PIX.
6. Tap **Enviar à cozinha** to print the ticket. Status becomes `cozinha`.
7. When the food is ready, mark it `pronto`, then `saiu` when the courier leaves. Copy the message for the client.
8. When the courier comes back and the order was delivered, record the payment if it isn't recorded yet. Tap **Fechar**.

## Flow 2: In-house

1. Open the app and tap **Novo pedido**.
2. Choose Balcão and enter the client name.
3. Add items: Adicionar item, then category, item, variant and quantity.
4. Tap **Enviar à cozinha** to print the ticket.
5. Mark the order as paid with the method used, for example credit card.
6. When the food is ready, mark it `pronto`.
7. Tap **Fechar**.

## Flow 3: WhatsApp, eat in

1. Open the app and tap **Novo pedido**.
2. Choose WhatsApp. Enter the client name and, optionally, the last 4 digits of the phone.
3. Add items: Adicionar item, then category, item, variant and quantity.
4. Tap **Copiar confirmação** and paste it into WhatsApp.
5. Choose **Consumir no local**.
6. Tap **Enviar à cozinha** to print the ticket.
7. When the food is ready, mark it `pronto` and copy the message for the client.
8. When the client has eaten and paid, record the payment. Tap **Fechar**.

## Messages

All messages are plain text with WhatsApp formatting (`*bold*`). Prices use `R$ 0,00`.

Confirmation:

```
Olá, Maria! Confirmando seu pedido:

2x HOT ROLL — R$ 30,00
1x COCA ZERO LT — R$ 6,00

*Total: R$ 36,00*
```

Ready (eat in): `Maria, seu pedido está pronto! 🍣`

Ready (retirada): `Maria, seu pedido está pronto para retirada! 🍣`

Out for delivery: `Maria, seu pedido saiu para entrega! 🛵`
