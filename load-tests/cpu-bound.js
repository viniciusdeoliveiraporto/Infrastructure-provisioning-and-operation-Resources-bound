import http from 'k6/http';
import { check } from 'k6';

export const options = {
  vus: __ENV.VUS || 5,
  duration: __ENV.DURATION || '60s',
};

const BASE_URL = __ENV.TARGET_URL || 'http://localhost:8080';
const ITERATIONS = __ENV.ITERATIONS || 1;

export default function () {
  const res = http.get(`${BASE_URL}/api/factor?number=999999999999&iterations=${ITERATIONS}`);
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
