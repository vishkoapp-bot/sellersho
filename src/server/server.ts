/**
 * @deprecated
 * DEPRECATED: This Express mock prototype server is retained for backward-compatibility only.
 * The production backend is the Go Modular Monolith running on port 8080 (backend/).
 * All API traffic is routed via Vite proxy '/api' -> http://localhost:8080.
 */
import express from 'express';
import cors from 'cors';
import dotenv from 'dotenv';
import { apiRouter } from './routes';

dotenv.config();

const app = express();
const port = process.env.PORT || 3001;

app.use(cors());
app.use(express.json());

// API Routes
app.use('/api', apiRouter);

// Start server
app.listen(port, () => {
  console.log(`Backend API Server running at http://localhost:${port}`);
  console.log(`- Health Check: http://localhost:${port}/api/health`);
  console.log(`- Products: http://localhost:${port}/api/products`);
  console.log(`- Stores: http://localhost:${port}/api/stores`);
});
