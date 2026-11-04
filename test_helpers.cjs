const http = require('http');
const fs = require('fs');

const BASE_URL = 'http://localhost:8080';
const HOST = 'localhost';
const PORT = 8080;

function request(method, path, body = null, token = null, extraHeaders = {}) {
  return new Promise((resolve) => {
    const startTime = Date.now();
    const headers = { ...extraHeaders };
    let data = null;

    if (body !== null) {
      if (typeof body === 'string') {
        data = body;
      } else {
        data = JSON.stringify(body);
        headers['Content-Type'] = 'application/json';
      }
      headers['Content-Length'] = Buffer.byteLength(data);
    }

    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    const req = http.request(
      {
        hostname: HOST,
        port: PORT,
        path: encodeURI(path),
        method: method,
        headers: headers,
        timeout: 6000,
      },
      (res) => {
        let respData = '';
        res.on('data', (chunk) => (respData += chunk));
        res.on('end', () => {
          const latency = Date.now() - startTime;
          let parsed = null;
          try {
            parsed = JSON.parse(respData);
          } catch {
            parsed = respData;
          }
          resolve({
            status: res.statusCode,
            body: parsed,
            rawBody: respData,
            latency,
          });
        });
      }
    );

    req.on('error', (err) => {
      resolve({
        status: 0,
        body: null,
        rawBody: err.message,
        latency: Date.now() - startTime,
        error: err.message,
      });
    });

    if (data) req.write(data);
    req.end();
  });
}

async function getAuthToken(mobile) {
  const sendRes = await request('POST', '/api/v1/auth/otp/send', { mobile });
  const demoCode = sendRes.body?.demoCode || '12345';
  const verifyRes = await request('POST', '/api/v1/auth/otp/verify', { mobile, code: demoCode });
  return verifyRes.body?.token;
}

module.exports = { request, getAuthToken, BASE_URL };
