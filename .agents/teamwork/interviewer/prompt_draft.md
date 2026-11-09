Use a full team ("یک تیم کامل"). Create comprehensive documentation and complete workflows for this platform in Persian, detailing all features and behaviors with concrete examples for each section.

Working directory: c:\Users\LENOVO\projects\sellersho
Integrity mode: development

## Requirements

### R1. Comprehensive Platform Documentation in Persian
Analyze the platform's codebase, data schemas, API routes, and components to create modular Persian documentation in `docs/fa/`:
- Document all technical and architectural aspects, including codebase structure, architectural patterns, database models/schemas, authentication, and core API endpoints/services.
- Document all business and user/merchant features, including storefront capabilities, seller dashboard, admin panel, product catalog management, order processing lifecycle, and payment/inventory workflows.
- For every section and feature documented, provide a clear, concrete example illustrating its behavior and real-world usage.

### R2. Complete Platform Workflows and Diagrams
Create end-to-end workflow documentation in Persian under `docs/fa/` detailing the system and user journeys:
- Provide visual workflow diagrams using Mermaid syntax for key workflows (e.g., customer purchase and checkout, seller onboarding and product listing, order fulfillment and status transitions, payment processing).
- Explain each step, role, and system behavior in each workflow with concrete examples.

## Acceptance Criteria

### Documentation Structure & Language
- [ ] All documentation files are written in Persian (Farsi) and located in `docs/fa/` with an `index.md` or `README.md` serving as the table of contents.
- [ ] Every platform feature, module, and behavior identified in the codebase is documented.
- [ ] Every documented section contains at least one clear, concrete example.

### Workflow & Technical Coverage
- [ ] Complete platform workflows are documented, including Mermaid sequence/flowchart diagrams for end-to-end user journeys (storefront customer, seller, and admin).
- [ ] Technical architecture (data models, API routes, authentication, services) is documented with accurate references to the platform codebase.
- [ ] No placeholder or unverified text exists in the generated documentation.
