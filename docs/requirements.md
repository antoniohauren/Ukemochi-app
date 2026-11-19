# Requirements

## Goal

Help the waitresses of a sushi restaurant record orders that arrive through WhatsApp or at the counter. Every order must print on a 58mm thermal printer.

## Global rules

- The interface is in pt-BR. Code and docs are in English.
- Mobile first: every screen must work one-handed on a phone.
- Use as little third-party code as possible. See [decisions](decisions.md).
- Installable as a PWA.

## Product

A sellable product has these fields:

| Field          | Required | Example       |
|----------------|----------|---------------|
| Category       | yes      | Refrigerante  |
| Item           | yes      | Coca-Cola     |
| Variant        | no       | Zero          |
| Size           | no       | Lata          |
| Printable name | yes      | COCA ZERO LT  |
| Description    | no       | 350 ml        |
| Price          | yes      | R$ 6,00       |

The printable name is short so it fits on a 58mm ticket (about 32 characters per line). See [data model](data-model.md) for how these fields map to tables.

## Helpers

- **Change calculator:** the waitress enters the cash amount and the app shows the change.
- **Copyable messages:** order confirmation, order ready and order out for delivery, formatted for WhatsApp.
- **Daily report:** order counts and totals for one day.

## Order flows

There are three waitress flows: WhatsApp delivery, in-house, and WhatsApp eat-in. See [flows](flows.md).

## Admin

The admin needs:

- The number of orders and total value per payment method (PIX, credit, debit, cash) for a chosen day.
- Menu management: categories, items and products.

Admin pages are protected by a single PIN.

## Out of scope (for now)

- Online ordering by customers.
- Stock control.
- Fiscal receipts (NFC-e).
- Multiple restaurants.
