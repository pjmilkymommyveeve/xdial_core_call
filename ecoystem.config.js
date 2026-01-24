module.exports = {
  apps: [
    {
      name: 'xdial-call-api',
      script: '/root/xdial_core_call/main',
      cwd: '/root/xdial_core_call/',
      instances: 1,              
      exec_mode: 'fork',         
      autorestart: true,
      watch: false,
      max_memory_restart: '500M',
      env: {
        PORT: '8080',
        DB_HOST: 'localhost',
        DB_PORT: '5432',
        DB_USER: 'xdialcore',
        DB_PASSWORD: 'xdialcore',
        DB_NAME: 'xdialcore',
        GOMAXPROCS: '1'
      },
      error_file: '/root/xdial_core_call/logs/error.log',
      out_file: '/root/xdial_core_call/logs/out.log',
      log_date_format: 'YYYY-MM-DD HH:mm:ss Z',
      merge_logs: true,
      min_uptime: '10s',
      max_restarts: 10,
      kill_timeout: 5000
    }
  ]
};

