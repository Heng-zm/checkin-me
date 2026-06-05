# Changelog

## v3 — bug fixes and performance improvements

### Fixed

- Payroll calculation no longer runs nested queries while employee rows are still open, preventing pgx connection-busy failures.
- Approved/paid payroll runs are protected from accidental recalculation.
- Face scan endpoint now requires `face_score`.
- Device webhook now rejects duplicate open clock-ins and reports missing open sessions for clock-out events.
- EWA availability now includes pending requests, not only approved ones.

### Improved

- Added panic recovery middleware and security headers.
- Added request JSON body limit and strict one-object JSON decoding.
- Added employee search/pagination.
- Added route-level privacy restrictions for employee, attendance, CRM, and sales data.
- Added latitude/longitude, GPS accuracy, QR TTL/radius, shift time, schedule date, salary, and currency validation.
- Added database pool tuning via env variables.
- Added indexes and DB constraints for high-traffic attendance, payroll, EWA, QR, and report queries.

### Notes

- Bank payout remains a draft/manual CSV adapter until a bank-approved API contract, sandbox credentials, signing method, idempotency rules, and callback format are provided.
- Payroll tax/NSSF defaults are editable rules and must be verified before production payroll.
