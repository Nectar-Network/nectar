

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
| keeper_registry | Analyzed | 3 | 30 | 0 | 5 | 


Issues found:



- [Dynamic Storage](#dynamic-storage) (2 results) (Medium)

- [Integer Overflow Or Underflow](#integer-overflow-or-underflow) (3 results) (Critical)

- [Soroban Version](#soroban-version) (29 results) (Enhancement)

- [Unnecessary Admin Parameter](#unnecessary-admin-parameter) (4 results) (Medium)



## Resource Management



### Dynamic Storage

**Impact:** Medium

**Issue:** Using dynamic types in instance or persistent storage can lead to unnecessary growth or storage-related vulnerabilities.

**Description:** Using dynamic types in instance or persistent storage can lead to unnecessary growth or storage-related vulnerabilities.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/dynamic-storage)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 3 | contracts | [lib.rs:117:9 - 117:48](contracts/keeper-registry/src/lib.rs) |
| 8 | contracts | [lib.rs:171:9 - 171:51](contracts/keeper-registry/src/lib.rs) |



## Arithmetic



### Integer Overflow Or Underflow

**Impact:** Critical

**Issue:** Potential for integer arithmetic overflow/underflow. Consider checked, wrapping or saturating arithmetic.

**Description:** An overflow/underflow is typically caught and generates an error. When it is not caught, the operation will result in an inexact result which could lead to serious problems.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/integer-overflow-or-underflow)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 7 | contracts | [lib.rs:121:43 - 121:54](contracts/keeper-registry/src/lib.rs) |
| 26 | contracts | [lib.rs:359:31 - 359:81](contracts/keeper-registry/src/lib.rs) |
| 27 | contracts | [lib.rs:367:13 - 367:36](contracts/keeper-registry/src/lib.rs) |



## Best Practices



### Soroban Version

**Impact:** Enhancement

**Issue:** Use the latest version of Soroban

**Description:** Using a older version of Soroban can be dangerous, as it may have bugs or security issues. Use the latest version available.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/soroban-version)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 0 | contracts | [lib.rs:1:1 - 1:1](contracts/keeper-registry/src/lib.rs) |


### Ineffective Extend Ttl

**Impact:** Medium

**Issue:** extend_ttl called with identical or smaller TTL arguments keeps refreshing the entry without enforcing expiration

**Description:** Soroban's extend_ttl can only increase an entry's lifetime. When both TTL parameters refer to the same binding, or the new TTL is smaller than the threshold, the call will run on every access making it ineffective

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/ineffective-extend-ttl)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 1 | contracts | [lib.rs:44:15 - 44:37](contracts/keeper-registry/src/lib.rs) |
| 2 | contracts | [lib.rs:52:34 - 52:56](contracts/keeper-registry/src/lib.rs) |
| 4 | contracts | [lib.rs:63:34 - 63:56](contracts/keeper-registry/src/lib.rs) |
| 5 | contracts | [lib.rs:111:16 - 111:78](contracts/keeper-registry/src/lib.rs) |
| 6 | contracts | [lib.rs:118:16 - 118:64](contracts/keeper-registry/src/lib.rs) |
| 9 | contracts | [lib.rs:132:34 - 132:56](contracts/keeper-registry/src/lib.rs) |
| 10 | contracts | [lib.rs:172:16 - 172:64](contracts/keeper-registry/src/lib.rs) |
| 11 | contracts | [lib.rs:186:34 - 186:56](contracts/keeper-registry/src/lib.rs) |
| 12 | contracts | [lib.rs:197:34 - 197:56](contracts/keeper-registry/src/lib.rs) |
| 13 | contracts | [lib.rs:221:34 - 221:56](contracts/keeper-registry/src/lib.rs) |
| 14 | contracts | [lib.rs:235:34 - 235:56](contracts/keeper-registry/src/lib.rs) |
| 15 | contracts | [lib.rs:243:34 - 243:56](contracts/keeper-registry/src/lib.rs) |
| 16 | contracts | [lib.rs:251:34 - 251:56](contracts/keeper-registry/src/lib.rs) |
| 17 | contracts | [lib.rs:258:34 - 258:56](contracts/keeper-registry/src/lib.rs) |
| 18 | contracts | [lib.rs:265:34 - 265:56](contracts/keeper-registry/src/lib.rs) |
| 19 | contracts | [lib.rs:276:16 - 276:76](contracts/keeper-registry/src/lib.rs) |
| 20 | contracts | [lib.rs:286:34 - 286:56](contracts/keeper-registry/src/lib.rs) |
| 21 | contracts | [lib.rs:296:16 - 296:76](contracts/keeper-registry/src/lib.rs) |
| 22 | contracts | [lib.rs:311:34 - 311:56](contracts/keeper-registry/src/lib.rs) |
| 23 | contracts | [lib.rs:329:16 - 329:76](contracts/keeper-registry/src/lib.rs) |
| 24 | contracts | [lib.rs:339:34 - 339:56](contracts/keeper-registry/src/lib.rs) |
| 25 | contracts | [lib.rs:378:16 - 378:76](contracts/keeper-registry/src/lib.rs) |
| 28 | contracts | [lib.rs:405:34 - 405:56](contracts/keeper-registry/src/lib.rs) |
| 29 | contracts | [lib.rs:420:34 - 420:56](contracts/keeper-registry/src/lib.rs) |


### Storage Change Events

**Impact:** Enhancement

**Issue:** Consider emiting an event when storage is modified

**Description:** Emiting an event when storage changes is a good practice to make the contracts more transparent and usable to its clients and observers

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/storage-change-events)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 30 | contracts | [lib.rs:404:5 - 404:93](contracts/keeper-registry/src/lib.rs) |
| 31 | contracts | [lib.rs:257:5 - 257:66](contracts/keeper-registry/src/lib.rs) |
| 32 | contracts | [lib.rs:51:5 - 51:84](contracts/keeper-registry/src/lib.rs) |
| 33 | contracts | [lib.rs:250:5 - 250:64](contracts/keeper-registry/src/lib.rs) |



## Authorization



### Unnecessary Admin Parameter

**Impact:** Medium

**Issue:** Usage of admin parameter might be unnecessary

**Description:** This function has an admin parameter that might be unnecessary. Consider retrieving the admin from storage instead.

[**Learn More**](https://coinfabrik.github.io/scout-audit/docs/detectors/soroban/unnecessary-admin-parameter)

#### Findings

| ID  | Package | File Location |
| --- | ------- | ------------- |
| 34 | contracts | [lib.rs:51:32 - 51:46](contracts/keeper-registry/src/lib.rs) |
| 35 | contracts | [lib.rs:250:28 - 250:42](contracts/keeper-registry/src/lib.rs) |
| 36 | contracts | [lib.rs:257:30 - 257:44](contracts/keeper-registry/src/lib.rs) |
| 37 | contracts | [lib.rs:404:33 - 404:47](contracts/keeper-registry/src/lib.rs) |


