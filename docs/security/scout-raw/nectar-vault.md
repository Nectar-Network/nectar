

<style>
.markdown-body table {min-width: 100%;width: 100%;display: table;}
thead {min-width: 100%;width: 100%;}
th {min-width: 60%;width: 60%;}
th:last-child {min-width: 20%;width: 20%;}
th:first-child {min-width: 20%;width: 20%;}
</style>



# Scout Report - Nectar - 2026-08-16

## Summary

| <span style="color:green">Crate</span> | <span style="color:green">Status</span> | <span style="color:green">Critical</span> | <span style="color:green">Medium</span> | <span style="color:green">Minor</span> | <span style="color:green">Enhancement</span> | 
| - | - | - | - | - | - | 
| nectar_vault | Analyzed | 24 | 27 | 0 | 4 | 


Issues found:



- [Unnecessary Admin Parameter](#unnecessary-admin-parameter) (5 results) (Medium)

- [Integer Overflow Or Underflow](#integer-overflow-or-underflow) (24 results) (Critical)

- [Soroban Version](#soroban-version) (26 results) (Enhancement)



## Authorization



### Unnecessary Admin Parameter

**Impact:** Medium

**Issue:** Usage of admin parameter might be unnecessary

**Description:** This function has an admin parameter that might be unnecessary. Consider retrieving the admin from storage instead.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/unnecessary-admin-parameter)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 50 | contracts | [lib.rs:518:33 - 518:47](contracts/nectar-vault/src/lib.rs) |
| 51 | contracts | [lib.rs:538:28 - 538:42](contracts/nectar-vault/src/lib.rs) |
| 52 | contracts | [lib.rs:583:9 - 583:23](contracts/nectar-vault/src/lib.rs) |
| 53 | contracts | [lib.rs:545:30 - 545:44](contracts/nectar-vault/src/lib.rs) |
| 54 | contracts | [lib.rs:563:39 - 563:53](contracts/nectar-vault/src/lib.rs) |



## Arithmetic



### Integer Overflow Or Underflow

**Impact:** Critical

**Issue:** Potential for integer arithmetic overflow/underflow. Consider checked, wrapping or saturating arithmetic.

**Description:** An overflow/underflow is typically caught and generates an error. When it is not caught, the operation will result in an inexact result which could lead to serious problems.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/integer-overflow-or-underflow)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 1 | contracts | [lib.rs:22:5 - 22:77](contracts/nectar-vault/src/lib.rs) |
| 2 | contracts | [lib.rs:27:5 - 27:77](contracts/nectar-vault/src/lib.rs) |
| 6 | contracts | [lib.rs:118:9 - 118:35](contracts/nectar-vault/src/lib.rs) |
| 7 | contracts | [lib.rs:125:9 - 125:35](contracts/nectar-vault/src/lib.rs) |
| 8 | contracts | [lib.rs:126:9 - 126:37](contracts/nectar-vault/src/lib.rs) |
| 11 | contracts | [lib.rs:196:16 - 196:83](contracts/nectar-vault/src/lib.rs) |
| 12 | contracts | [lib.rs:199:13 - 199:54](contracts/nectar-vault/src/lib.rs) |
| 13 | contracts | [lib.rs:204:9 - 204:35](contracts/nectar-vault/src/lib.rs) |
| 14 | contracts | [lib.rs:210:9 - 210:37](contracts/nectar-vault/src/lib.rs) |
| 15 | contracts | [lib.rs:211:9 - 211:37](contracts/nectar-vault/src/lib.rs) |
| 20 | contracts | [lib.rs:292:25 - 292:60](contracts/nectar-vault/src/lib.rs) |
| 21 | contracts | [lib.rs:323:25 - 323:38](contracts/nectar-vault/src/lib.rs) |
| 22 | contracts | [lib.rs:330:9 - 330:35](contracts/nectar-vault/src/lib.rs) |
| 25 | contracts | [lib.rs:405:22 - 405:43](contracts/nectar-vault/src/lib.rs) |
| 26 | contracts | [lib.rs:407:9 - 407:34](contracts/nectar-vault/src/lib.rs) |
| 27 | contracts | [lib.rs:408:9 - 408:35](contracts/nectar-vault/src/lib.rs) |
| 28 | contracts | [lib.rs:409:9 - 409:37](contracts/nectar-vault/src/lib.rs) |
| 29 | contracts | [lib.rs:413:29 - 413:49](contracts/nectar-vault/src/lib.rs) |
| 31 | contracts | [lib.rs:493:9 - 493:35](contracts/nectar-vault/src/lib.rs) |
| 32 | contracts | [lib.rs:494:9 - 494:37](contracts/nectar-vault/src/lib.rs) |
| 41 | contracts | [lib.rs:653:9 - 653:34](contracts/nectar-vault/src/lib.rs) |
| 42 | contracts | [lib.rs:665:20 - 665:43](contracts/nectar-vault/src/lib.rs) |
| 43 | contracts | [lib.rs:666:28 - 666:53](contracts/nectar-vault/src/lib.rs) |
| 44 | contracts | [lib.rs:667:9 - 667:35](contracts/nectar-vault/src/lib.rs) |



## Best Practices



### Soroban Version

**Impact:** Enhancement

**Issue:** Use the latest version of Soroban

**Description:** Using a older version of Soroban can be dangerous, as it may have bugs or security issues. Use the latest version available.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/soroban-version)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 0 | contracts | [lib.rs:1:1 - 1:1](contracts/nectar-vault/src/lib.rs) |


### Ineffective Extend Ttl

**Impact:** Medium

**Issue:** extend_ttl called with identical or smaller TTL arguments keeps refreshing the entry without enforcing expiration

**Description:** Soroban's extend_ttl can only increase an entry's lifetime. When both TTL parameters refer to the same binding, or the new TTL is smaller than the threshold, the call will run on every access making it ineffective

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/ineffective-extend-ttl)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 3 | contracts | [lib.rs:46:34 - 46:56](contracts/nectar-vault/src/lib.rs) |
| 4 | contracts | [lib.rs:68:34 - 68:56](contracts/nectar-vault/src/lib.rs) |
| 5 | contracts | [lib.rs:123:14 - 123:56](contracts/nectar-vault/src/lib.rs) |
| 9 | contracts | [lib.rs:138:34 - 138:56](contracts/nectar-vault/src/lib.rs) |
| 10 | contracts | [lib.rs:208:14 - 208:56](contracts/nectar-vault/src/lib.rs) |
| 16 | contracts | [lib.rs:230:34 - 230:56](contracts/nectar-vault/src/lib.rs) |
| 17 | contracts | [lib.rs:240:22 - 240:64](contracts/nectar-vault/src/lib.rs) |
| 18 | contracts | [lib.rs:263:34 - 263:56](contracts/nectar-vault/src/lib.rs) |
| 19 | contracts | [lib.rs:329:14 - 329:51](contracts/nectar-vault/src/lib.rs) |
| 23 | contracts | [lib.rs:362:34 - 362:56](contracts/nectar-vault/src/lib.rs) |
| 24 | contracts | [lib.rs:430:22 - 430:59](contracts/nectar-vault/src/lib.rs) |
| 30 | contracts | [lib.rs:464:34 - 464:56](contracts/nectar-vault/src/lib.rs) |
| 33 | contracts | [lib.rs:503:34 - 503:56](contracts/nectar-vault/src/lib.rs) |
| 34 | contracts | [lib.rs:511:34 - 511:56](contracts/nectar-vault/src/lib.rs) |
| 35 | contracts | [lib.rs:519:34 - 519:56](contracts/nectar-vault/src/lib.rs) |
| 36 | contracts | [lib.rs:539:34 - 539:56](contracts/nectar-vault/src/lib.rs) |
| 37 | contracts | [lib.rs:546:34 - 546:56](contracts/nectar-vault/src/lib.rs) |
| 38 | contracts | [lib.rs:564:34 - 564:56](contracts/nectar-vault/src/lib.rs) |
| 39 | contracts | [lib.rs:587:34 - 587:56](contracts/nectar-vault/src/lib.rs) |
| 40 | contracts | [lib.rs:626:34 - 626:56](contracts/nectar-vault/src/lib.rs) |
| 45 | contracts | [lib.rs:680:34 - 680:56](contracts/nectar-vault/src/lib.rs) |
| 46 | contracts | [lib.rs:695:34 - 695:56](contracts/nectar-vault/src/lib.rs) |


### Storage Change Events

**Impact:** Enhancement

**Issue:** Consider emiting an event when storage is modified

**Description:** Emiting an event when storage changes is a good practice to make the contracts more transparent and usable to its clients and observers

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/storage-change-events)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 47 | contracts | [lib.rs:538:5 - 538:69](contracts/nectar-vault/src/lib.rs) |
| 48 | contracts | [lib.rs:545:5 - 545:71](contracts/nectar-vault/src/lib.rs) |
| 49 | contracts | [lib.rs:518:5 - 518:95](contracts/nectar-vault/src/lib.rs) |


