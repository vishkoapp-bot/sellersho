# Original User Request

## Approved Task Prompt — 2026-11-07T17:55:41Z

Redesign the fashion storefront template (`FashionTemplate`) in the Sellersho dropshipping platform with a modern, high-quality visual design and ensure full mobile and desktop responsiveness.

Working directory: C:\Users\LENOVO\projects\sellersho
Integrity mode: development

## Requirements

### R1. Fashion Storefront Template Redesign
Redesign the fashion storefront template (`src/components/storefront/templates/FashionTemplate.tsx` and related components in `src/components/storefront/`) with an elevated visual presentation suitable for a fashion and apparel store. All existing storefront functionality, props, categories, product grids, banners, and interactions must be preserved.

### R2. Responsive Layout Optimization
Ensure the template adapts cleanly across mobile, tablet, and desktop screens without horizontal overflow, clipped content, or broken layouts.

### R3. Production Build and Server Deployment
Commit the changes to Git (`vishkoapp-bot/sellersho`), verify that the project builds cleanly with `npm run build`, and deploy the updated application to the ParsPack production server at IP 46.249.98.190 in `/opt/drup`.

## Acceptance Criteria

### Visual Redesign & Functionality
- [ ] The fashion storefront template renders an overhauled, modern visual design for hero sections, banners, category displays, and product cards.
- [ ] All existing template props, handlers, and dynamic data bindings (products, categories, store settings) function without regression.
- [ ] Template registry correctly loads the redesigned Fashion template without console errors.

### Responsiveness
- [ ] Layout displays without horizontal scrollbars or clipping at mobile viewports (375px and 390px).
- [ ] Layout displays cleanly at tablet viewports (768px).
- [ ] Layout displays cleanly at desktop viewports (1280px and wider).
- [ ] Interactive elements (buttons, navigation, filters, product cards) have accessible touch targets on mobile viewports.

### Build & Deployment
- [ ] `npm run build` completes successfully with exit code 0.
- [ ] Changes are committed to Git.
- [ ] The build is deployed to the production server at 46.249.98.190 (`/opt/drup`).
