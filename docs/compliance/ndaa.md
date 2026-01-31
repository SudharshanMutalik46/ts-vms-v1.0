# NDAA Compliance Requirements

## 1. Scope
**Regulation:** Section 889 of the 2019 National Defense Authorization Act (NDAA).
**Constraint:** "No Covered Telecommunications or Video Surveillance Equipment or Services."

## 2. Policy
The VMS shall NOT include hardware, software, or services produced by:
- Huawei Technologies Company
- ZTE Corporation
- Hytera Communications Corporation
- Hangzhou Hikvision Digital Technology Company
- Dahua Technology Company
- (Or any subsidiary/affiliate of these entities)

## 3. Process
1. **Restricted Vendor List:** Managed by Procurement. Updated quarterly.
2. **Supplier Attestation:** Every hardware supplier MUST sign an "NDAA Section 889 Representation" form.
3. **Verification:**
    - **Initial:** Check BOM against Restricted List.
    - **Recurring:** Audit new dependencies during release.

## 4. Documentation Requirements
Per component, we must archive:
- Supplier Name & Country of Origin.
- Signed "Non-Presence" Attestation.
- Chipset/SoC Manufacturer details (for cameras/NVRs).

**Dependencies**: [Phase 0.4 Standards](/docs/standards), [Phase 0.5 Security](/docs/security).
