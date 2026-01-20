# SMTP Protocol State Machine

This document describes the SMTP protocol state machine implemented by icesmtp.

## States

The SMTP session moves through the following states:

| State | Description |
|-------|-------------|
| `Disconnected` | No active connection |
| `Connected` | TCP connection established, greeting not sent |
| `Greeted` | Server has sent 220 greeting |
| `Identified` | Client has sent HELO/EHLO |
| `MailFrom` | MAIL FROM accepted, awaiting RCPT TO |
| `RcptTo` | At least one RCPT TO accepted |
| `Data` | DATA command accepted, receiving message |
| `DataDone` | Message data received, transaction complete |
| `StartTLS` | STARTTLS initiated, TLS handshake in progress |
| `Terminating` | QUIT received, sending 221 |
| `Terminated` | Session ended normally |
| `Aborted` | Session forcibly terminated |

## State Diagram

```
                                    +---------------+
                                    | Disconnected  |
                                    +-------+-------+
                                            |
                                     Connect|
                                            v
                                    +-------+-------+
                                    |   Connected   |
                                    +-------+-------+
                                            |
                                     220 Greeting
                                            v
                                    +-------+-------+
                          +-------->|    Greeted    |<---------+
                          |         +-------+-------+          |
                          |                 |                  |
                          |          HELO/EHLO                 |
                          |                 v                  |
                          |         +-------+-------+          |
                          |  +----->|  Identified   |<----+    |
                          |  |      +-------+-------+     |    |
                          |  |              |             |    |
                    STARTTLS |        MAIL FROM           |    |
                    complete |              v             |    |
                          |  |      +-------+-------+     |    |
                          |  |      |   MailFrom    |     |    |
                          |  |      +-------+-------+     |    |
                          |  |              |             |    |
                          |  |         RCPT TO         RSET    |
                          |  |              v             |    |
                          |  |      +-------+-------+     |    |
                          |  |      |    RcptTo     +-----+    |
                          |  |      +-------+-------+          |
                          |  |              |                  |
                          |  |           DATA                  |
                          |  |              v                  |
                          |  |      +-------+-------+          |
                          |  |      |     Data      |          |
                          |  |      +-------+-------+          |
                          |  |              |                  |
                          |  |      <CRLF>.<CRLF>              |
                          |  |              v                  |
                          |  |      +-------+-------+          |
                          |  +------+   DataDone    +----------+
                          |         +---------------+
                          |
                    +-----+------+
                    |  StartTLS  |
                    +------------+
```

## Valid Transitions

### From Disconnected
- `Connected` - TCP connection established

### From Connected
- `Greeted` - Server sends 220 greeting
- `Terminated` - Connection closed
- `Aborted` - Error or timeout

### From Greeted
- `Identified` - HELO or EHLO accepted
- `Terminating` - QUIT received
- `Aborted` - Error or policy violation

### From Identified
- `Identified` - HELO/EHLO can be repeated
- `MailFrom` - MAIL FROM accepted
- `StartTLS` - STARTTLS initiated
- `Terminating` - QUIT received
- `Aborted` - Error or policy violation

### From MailFrom
- `RcptTo` - RCPT TO accepted
- `Identified` - RSET received
- `Terminating` - QUIT received
- `Aborted` - Error or policy violation

### From RcptTo
- `RcptTo` - Additional RCPT TO accepted
- `Data` - DATA command accepted
- `Identified` - RSET received
- `Terminating` - QUIT received
- `Aborted` - Error or policy violation

### From Data
- `DataDone` - Message terminator received
- `Aborted` - Error or timeout

### From DataDone
- `Identified` - Ready for new transaction
- `Terminating` - QUIT received
- `Aborted` - Error

### From StartTLS
- `Greeted` - TLS handshake complete (client must re-EHLO)
- `Aborted` - TLS handshake failed

### From Terminating
- `Terminated` - 221 sent, connection closed

### Terminal States
- `Terminated` - Normal session end
- `Aborted` - Abnormal session end

## Command Validity by State

| Command | Greeted | Identified | MailFrom | RcptTo | Data |
|---------|---------|------------|----------|--------|------|
| HELO    | Yes     | Yes        | No       | No     | No   |
| EHLO    | Yes     | Yes        | No       | No     | No   |
| MAIL    | No      | Yes        | No       | No     | No   |
| RCPT    | No      | No         | Yes      | Yes    | No   |
| DATA    | No      | No         | No       | Yes    | No   |
| RSET    | Yes     | Yes        | Yes      | Yes    | No   |
| NOOP    | Yes     | Yes        | Yes      | Yes    | No   |
| QUIT    | Yes     | Yes        | Yes      | Yes    | No   |
| VRFY    | No      | Yes        | No       | No     | No   |
| EXPN    | No      | Yes        | No       | No     | No   |
| HELP    | Yes     | Yes        | Yes      | Yes    | No   |
| STARTTLS| No      | Yes        | No       | No     | No   |
| AUTH    | No      | Yes        | No       | No     | No   |

## Reply Codes

| Code | Meaning |
|------|---------|
| 220  | Service ready |
| 221  | Service closing |
| 250  | OK |
| 251  | User not local; will forward |
| 252  | Cannot VRFY user |
| 354  | Start mail input |
| 421  | Service not available |
| 450  | Mailbox unavailable (transient) |
| 451  | Local error |
| 452  | Insufficient storage |
| 500  | Syntax error |
| 501  | Syntax error in parameters |
| 502  | Command not implemented |
| 503  | Bad sequence of commands |
| 504  | Parameter not implemented |
| 550  | Mailbox unavailable (permanent) |
| 551  | User not local |
| 552  | Exceeded storage allocation |
| 553  | Mailbox name invalid |
| 554  | Transaction failed |
