# Order flows

## Order attributes

- `channel`: `whatsapp` or `balcao` (in-house).
- `consume`: `entrega` (delivery) or `local` (eat in).
- `status`: see the state machine below.
- payment: zero or more parts, each with a method (`pix`, `credito`, `debito`, `dinheiro`) and an amount. Unpaid with no parts, paid when the parts add up to the total.

## State machine

```
aberto -> cozinha -> pronto -> saiu -> fechado
   \         \          \         \      ^
    \         \          +---------\-----+   (consume = local)
     +---------+----------+---------+-> cancelado

fechado, cancelado -> aberto   (reopen)
cozinha, pronto    -> aberto   (an item is added, removed or changed)
```

| Status      | Meaning                                  |
|-------------|------------------------------------------|
| `aberto`    | Being created or edited                  |
| `cozinha`   | Sent to the kitchen and ticket printed   |
| `pronto`    | Food is ready                            |
| `saiu`      | Courier left (delivery only)             |
| `fechado`   | Finished                                 |
| `cancelado` | Cancelled                                |

### Rules

- Payment is independent of status. Parts can be added or removed at any status except `fechado` and `cancelado`.
- The bill can be split: pay all that is left (the default), a typed amount, or an equal share picked with **Dividir entre** (number of people). Each part has its own method. A part can't be more than what is left.
- A `fechado` or `cancelado` order can be reopened at any time. It goes back to `aberto`, keeps its payment and leaves the daily report until it is closed again.
- An order can't be closed until its parts add up to the total exactly (none missing, none over after items were removed).
- `saiu` exists only when `consume = entrega`.
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

Out for delivery: `Maria, seu pedido saiu para entrega! 🛵`
