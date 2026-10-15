
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const CSV_NAME = __ENV.CSV_FILE || 'dados1_1MB.csv';
const CSV_BODY = open(`./csvs/${CSV_NAME}`, 'b');

export const options = {
  vus: Number(__ENV.VUS || 5),
  duration: __ENV.DURATION || '60s',
};

export default function () {
  const response = http.post(
    `${BASE_URL}/files`,
    {
      file: http.file(CSV_BODY, CSV_NAME, 'text/csv'),
    }
  );

  check(response, {
    'upload respondeu com sucesso': (r) =>
      r.status >= 200 && r.status < 300,
  });

  sleep(1);
}
